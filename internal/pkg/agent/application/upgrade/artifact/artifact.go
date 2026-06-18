// Copyright Elasticsearch B.V. and/or licensed to Elasticsearch B.V. under one
// or more contributor license agreements. Licensed under the Elastic License 2.0;
// you may not use this file except in compliance with the Elastic License 2.0.

package artifact

import (
	"fmt"
	"path/filepath"

	"github.com/elastic/elastic-agent/internal/pkg/agent/errors"
	agtversion "github.com/elastic/elastic-agent/pkg/version"
)

var packageArchMap = map[string]string{
	"linux-binary-32":         "linux-x86.tar.gz",
	"linux-binary-64":         "linux-x86_64.tar.gz",
	"linux-binary-arm64":      "linux-arm64.tar.gz",
	"windows-binary-32":       "windows-x86.zip",
	"windows-binary-64":       "windows-x86_64.zip",
	"windows-binary-arm64":    "windows-arm64.zip",
	"darwin-binary-32":        "darwin-x86_64.tar.gz",
	"darwin-binary-64":        "darwin-x86_64.tar.gz",
	"darwin-binary-arm64":     "darwin-aarch64.tar.gz",
	"darwin-binary-universal": "darwin-universal.tar.gz",
}

// Artifact provides info for fetching from artifact store.
type Artifact struct {
	Name     string
	Cmd      string
	Artifact string
	Version  *agtversion.ParsedSemVer
	Filename string
	FilePath string
}

func New(version *agtversion.ParsedSemVer, settings *Config, fips bool) (Artifact, error) {
	var cmd string
	if fips {
		cmd = "elastic-agent-fips"
	} else {
		cmd = "elastic-agent"
	}

	key := fmt.Sprintf("%s-binary-%s", settings.OS(), settings.Arch())
	suffix, found := packageArchMap[key]
	if !found {
		return Artifact{}, errors.New(fmt.Sprintf("'%s' is not a valid combination for a package", key), errors.TypeConfig)
	}

	filename := fmt.Sprintf("%s-%s-%s", cmd, version.VersionWithPrerelease(), suffix)

	return Artifact{
		Name:     "Elastic Agent",
		Cmd:      cmd,
		Artifact: "beats/elastic-agent",
		Version:  version,
		Filename: filename,
		FilePath: filepath.Join(settings.TargetDirectory, filename),
	}, nil
}
