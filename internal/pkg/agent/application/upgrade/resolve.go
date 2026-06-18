// Copyright Elasticsearch B.V. and/or licensed to Elasticsearch B.V. under one
// or more contributor license agreements. Licensed under the Elastic License 2.0;
// you may not use this file except in compliance with the Elastic License 2.0.

package upgrade

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/cenkalti/backoff/v4"

	"github.com/elastic/elastic-agent/internal/pkg/agent/application/upgrade/artifact"
	"github.com/elastic/elastic-agent/internal/pkg/release"
	agtversion "github.com/elastic/elastic-agent/pkg/version"
)

const snapshotURIFormat = "https://snapshots.elastic.co/%s-%s/downloads/"

type Resolver struct{}

func (Resolver) Resolve(ctx context.Context, client *http.Client, a artifact.Artifact, sourceURI string) (string, error) {
	if strings.HasPrefix(sourceURI, "file://") {
		return strings.TrimRight(sourceURI, "/") + "/" + a.Filename, nil
	}

	base := sourceURI
	if a.Version.IsSnapshot() {
		resolved, err := resolveSnapshotSourceURI(ctx, client, sourceURI, a.Version, a.Version.BuildMetadata())
		if err != nil {
			return "", fmt.Errorf("resolving snapshot source URI: %w", err)
		}
		base = resolved
	}

	return strings.TrimRight(base, "/") + "/" + a.Artifact + "/" + a.Filename, nil
}

func resolveSnapshotSourceURI(ctx context.Context, client *http.Client, sourceURI string, version *agtversion.ParsedSemVer, buildID string) (string, error) {
	if sourceURI != artifact.DefaultSourceURI {
		return sourceURI, nil
	}

	versionStr := release.Version()
	if version != nil {
		if buildID != "" {
			return fmt.Sprintf(snapshotURIFormat, version.CoreVersion(), buildID), nil
		}
		versionStr = version.CoreVersion()
	}

	latestSnapshotURI := fmt.Sprintf("https://snapshots.elastic.co/latest/%s-SNAPSHOT.json", versionStr)

	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()

	var snapshotBuildID string
	op := func() error {
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, latestSnapshotURI, nil)
		if err != nil {
			return backoff.Permanent(fmt.Errorf("failed to create request to the snapshot API: %w", err))
		}
		resp, err := client.Do(req)
		if err != nil {
			return err
		}
		defer resp.Body.Close()

		switch resp.StatusCode {
		case http.StatusNotFound:
			return backoff.Permanent(fmt.Errorf("snapshot for version %q not found", versionStr))
		case http.StatusOK:
			var info struct {
				BuildID string `json:"build_id"`
			}
			if err := json.NewDecoder(resp.Body).Decode(&info); err != nil {
				return backoff.Permanent(err)
			}
			parts := strings.Split(info.BuildID, "-")
			if len(parts) != 2 {
				return backoff.Permanent(fmt.Errorf("wrong format for a build ID: %s", info.BuildID))
			}
			snapshotBuildID = parts[1]
			return nil
		default:
			return fmt.Errorf("unexpected status code %d from %s", resp.StatusCode, latestSnapshotURI)
		}
	}

	if err := backoff.Retry(op, backoff.WithContext(backoff.NewExponentialBackOff(), ctx)); err != nil {
		return "", err
	}

	return fmt.Sprintf(snapshotURIFormat, versionStr, snapshotBuildID), nil
}
