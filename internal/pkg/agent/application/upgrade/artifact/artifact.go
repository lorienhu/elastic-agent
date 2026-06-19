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

var packageArchMap = map[struct{ os, arch string }]string{
	{"linux", "386"}:     "linux-x86.tar.gz",
	{"linux", "amd64"}:   "linux-x86_64.tar.gz",
	{"linux", "arm64"}:   "linux-arm64.tar.gz",
	{"windows", "386"}:   "windows-x86.zip",
	{"windows", "amd64"}: "windows-x86_64.zip",
	{"windows", "arm64"}: "windows-arm64.zip",
	{"darwin", "amd64"}:  "darwin-x86_64.tar.gz",
	{"darwin", "arm64"}:  "darwin-aarch64.tar.gz",
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

func New(version *agtversion.ParsedSemVer, settings *Config, os, arch string, fips bool) (Artifact, error) {
	var cmd string
	if fips {
		cmd = "elastic-agent-fips"
	} else {
		cmd = "elastic-agent"
	}

	suffix, found := packageArchMap[struct{ os, arch string }{os, arch}]
	if !found {
		return Artifact{}, errors.New(fmt.Sprintf("'%s/%s' is not a valid combination for a package", os, arch), errors.TypeConfig)
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
