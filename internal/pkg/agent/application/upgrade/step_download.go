// Copyright Elasticsearch B.V. and/or licensed to Elasticsearch B.V. under one
// or more contributor license agreements. Licensed under the Elastic License 2.0;
// you may not use this file except in compliance with the Elastic License 2.0.

package upgrade

import (
	"context"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/cenkalti/backoff/v4"

	"go.elastic.co/apm/v2"

	"github.com/elastic/elastic-agent/internal/pkg/agent/application/paths"
	"github.com/elastic/elastic-agent/internal/pkg/agent/application/upgrade/artifact"
	"github.com/elastic/elastic-agent/internal/pkg/agent/application/upgrade/artifact/download"
	downloadErrors "github.com/elastic/elastic-agent/internal/pkg/agent/application/upgrade/artifact/download/errors"
	"github.com/elastic/elastic-agent/internal/pkg/agent/application/upgrade/artifact/download/fs"
	"github.com/elastic/elastic-agent/internal/pkg/agent/application/upgrade/artifact/download/localremote"
	"github.com/elastic/elastic-agent/internal/pkg/agent/errors"
	"github.com/elastic/elastic-agent/internal/pkg/release"
	"github.com/elastic/elastic-agent/pkg/core/logger"
	"github.com/elastic/elastic-agent/pkg/upgrade/details"
	agtversion "github.com/elastic/elastic-agent/pkg/version"
)

const (
	defaultUpgradeFallbackPGP     = "https://artifacts.elastic.co/GPG-KEY-elastic-agent"
	fleetUpgradeFallbackPGPFormat = "/api/agents/upgrades/%d.%d.%d/pgp-public-key"
)

type downloaderFactory func(*logger.Logger, *artifact.Config, *details.Details) (download.Downloader, error)

type downloader func(context.Context, downloaderFactory, artifact.Artifact, *artifact.Config, string, *details.Details) (string, error)

// abstraction for testability for the verifier constructor
type verifierFactory func(*logger.Logger, *artifact.Config, []byte) (download.Verifier, error)

type artifactDownloader struct {
	log            *logger.Logger
	settings       *artifact.Config
	fleetServerURI string
	newVerifier    verifierFactory
}

func newArtifactDownloader(settings *artifact.Config, log *logger.Logger) *artifactDownloader {
	return &artifactDownloader{
		log:         log,
		settings:    settings,
		newVerifier: localremote.NewVerifier,
	}
}

func (a *artifactDownloader) withFleetServerURI(fleetServerURI string) {
	a.fleetServerURI = fleetServerURI
}

func (a *artifactDownloader) downloadArtifact(ctx context.Context, agentArtifact artifact.Artifact, sourceURI string, upgradeDetails *details.Details, skipVerifyOverride, skipDefaultPgp bool, pgpBytes ...string) (_ string, err error) {
	span, ctx := apm.StartSpan(ctx, "downloadArtifact", "app.internal")
	defer func() {
		apm.CaptureError(ctx, err).Send()
		span.End()
	}()

	pgpBytes = a.appendFallbackPGP(agentArtifact.Version, pgpBytes)

	// do not update source config
	settings := *a.settings
	var downloaderFunc downloader
	var factory downloaderFactory
	var verifier download.Verifier
	if sourceURI != "" {
		if strings.HasPrefix(sourceURI, "file://") {
			// update the DropPath so the fs.Downloader can download from this
			// path instead of looking into the installed downloads directory
			settings.DropPath = filepath.Dir(strings.TrimPrefix(sourceURI, "file://"))

			// use specific function that doesn't perform retries on download as its
			// local and no retry should be performed
			downloaderFunc = a.downloadOnce

			// set specific downloader, local file just uses the fs.NewDownloader
			// no fallback is allowed because it was requested that this specific source be used
			factory = func(l *logger.Logger, config *artifact.Config, d *details.Details) (download.Downloader, error) {
				return fs.NewDownloader(config), nil
			}

			// set specific verifier, local file verifies locally only
			verifier, err = fs.NewVerifier(a.log, &settings, release.PGP())
			if err != nil {
				return "", errors.New(err, "initiating verifier")
			}

			// log that a local upgrade artifact is being used
			a.log.Infow("Using local upgrade artifact", "version", agentArtifact.Version,
				"drop_path", settings.DropPath,
				"target_path", settings.TargetDirectory, "install_path", settings.InstallPath)
		}
	}

	if factory == nil {
		// set the factory to the localremote downloader factory
		factory = localremote.NewDownloader
		a.log.Infow("Downloading upgrade artifact", "version", agentArtifact.Version,
			"source_uri", sourceURI, "drop_path", settings.DropPath,
			"target_path", settings.TargetDirectory, "install_path", settings.InstallPath, "proxy_uri", settings.Proxy.URL, "proxy_disable", settings.Proxy.Disable)
	}
	if downloaderFunc == nil {
		downloaderFunc = a.downloadWithRetries
	}

	if err := os.MkdirAll(paths.Downloads(), 0750); err != nil {
		return "", fmt.Errorf("failed to create download directory at %s: %w", paths.Downloads(), err)
	}

	path, err := downloaderFunc(ctx, factory, agentArtifact, &settings, sourceURI, upgradeDetails)
	if err != nil {
		return "", fmt.Errorf("failed download of agent binary: %w", err)
	}

	// If there are errors in the following steps, we return the path so that we
	// can cleanup the downloaded files.
	if skipVerifyOverride {
		return path, nil
	}

	if verifier == nil {
		verifier, err = a.newVerifier(a.log, &settings, release.PGP())
		if err != nil {
			return path, errors.New(err, "initiating verifier")
		}
	}

	if err := verifier.Verify(ctx, agentArtifact, sourceURI, skipDefaultPgp, pgpBytes...); err != nil {
		return path, errors.New(err, "failed verification of agent binary")
	}
	return path, nil
}

func (a *artifactDownloader) appendFallbackPGP(targetVersion *agtversion.ParsedSemVer, pgpBytes []string) []string {
	if pgpBytes == nil {
		pgpBytes = make([]string, 0, 1)
	}

	fallbackPGP := download.PgpSourceURIPrefix + defaultUpgradeFallbackPGP
	pgpBytes = append(pgpBytes, fallbackPGP)

	// add a secondary fallback if fleet server is configured
	a.log.Debugf("Considering fleet server uri for pgp check fallback %q", a.fleetServerURI)
	if a.fleetServerURI != "" {
		secondaryPath, err := url.JoinPath(
			a.fleetServerURI,
			fmt.Sprintf(fleetUpgradeFallbackPGPFormat, targetVersion.Major(), targetVersion.Minor(), targetVersion.Patch()),
		)
		if err != nil {
			a.log.Warnf("failed to compose Fleet Server URI: %v", err)
		} else {
			secondaryFallback := download.PgpSourceURIPrefix + secondaryPath
			pgpBytes = append(pgpBytes, secondaryFallback)
		}
	}

	return pgpBytes
}

func (a *artifactDownloader) downloadOnce(
	ctx context.Context,
	factory downloaderFactory,
	agentArtifact artifact.Artifact,
	settings *artifact.Config,
	uri string,
	upgradeDetails *details.Details,
) (string, error) {
	downloader, err := factory(a.log, settings, upgradeDetails)
	if err != nil {
		return "", fmt.Errorf("unable to create fetcher: %w", err)
	}
	path, err := downloader.Download(ctx, agentArtifact, uri)
	if err != nil {
		return "", fmt.Errorf("unable to download package: %w", err)
	}

	// Download successful
	return path, nil
}

func (a *artifactDownloader) downloadWithRetries(
	ctx context.Context,
	factory downloaderFactory,
	agentArtifact artifact.Artifact,
	settings *artifact.Config,
	uri string,
	upgradeDetails *details.Details,
) (string, error) {
	cancelDeadline := time.Now().Add(settings.Timeout)
	cancelCtx, cancel := context.WithDeadline(ctx, cancelDeadline)
	defer cancel()

	upgradeDetails.SetRetryUntil(&cancelDeadline)

	expBo := backoff.NewExponentialBackOff()
	expBo.InitialInterval = settings.RetrySleepInitDuration
	boCtx := backoff.WithContext(expBo, cancelCtx)

	var path string
	var attempt uint

	opFn := func() error {
		attempt++
		a.log.Infof("download attempt %d", attempt)
		var err error
		path, err = a.downloadOnce(cancelCtx, factory, agentArtifact, settings, uri, upgradeDetails)
		if err != nil {
			if downloadErrors.IsDiskSpaceError(err) {
				a.log.Infof("insufficient disk space error detected, stopping retries")
				return backoff.Permanent(err)
			}
			return err
		}
		return nil
	}

	opFailureNotificationFn := func(err error, retryAfter time.Duration) {
		a.log.Warnf("download attempt %d failed: %s; retrying in %s.",
			attempt, err.Error(), retryAfter)
		upgradeDetails.SetRetryableError(err)
	}

	if err := backoff.RetryNotify(opFn, boCtx, opFailureNotificationFn); err != nil {
		return "", err
	}

	// Clear retry details upon success
	upgradeDetails.SetRetryableError(nil)
	upgradeDetails.SetRetryUntil(nil)

	return path, nil
}
