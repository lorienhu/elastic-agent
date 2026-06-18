// Copyright Elasticsearch B.V. and/or licensed to Elasticsearch B.V. under one
// or more contributor license agreements. Licensed under the Elastic License 2.0;
// you may not use this file except in compliance with the Elastic License 2.0.

package artifact

import (
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"

	agtversion "github.com/elastic/elastic-agent/pkg/version"
)

func TestNew(t *testing.T) {
	tests := map[string]struct {
		version      *agtversion.ParsedSemVer
		os           string
		arch         string
		fips         bool
		expectedName string
	}{
		"linux_arm64": {
			version:      agtversion.NewParsedSemVer(9, 1, 0, "", ""),
			os:           "linux",
			arch:         "arm64",
			expectedName: "elastic-agent-9.1.0-linux-arm64.tar.gz",
		},
		"fips": {
			version:      agtversion.NewParsedSemVer(9, 1, 0, "", ""),
			os:           "linux",
			arch:         "64",
			fips:         true,
			expectedName: "elastic-agent-fips-9.1.0-linux-x86_64.tar.gz",
		},
		"linux_x86": {
			version:      agtversion.NewParsedSemVer(9, 1, 0, "", ""),
			os:           "linux",
			arch:         "32",
			expectedName: "elastic-agent-9.1.0-linux-x86.tar.gz",
		},
		"linux_x86_64": {
			version:      agtversion.NewParsedSemVer(9, 1, 0, "", ""),
			os:           "linux",
			arch:         "64",
			expectedName: "elastic-agent-9.1.0-linux-x86_64.tar.gz",
		},
		"snapshot": {
			version:      agtversion.NewParsedSemVer(1, 2, 3, "SNAPSHOT", ""),
			os:           "linux",
			arch:         "64",
			expectedName: "elastic-agent-1.2.3-SNAPSHOT-linux-x86_64.tar.gz",
		},
		"build metadata is dropped": {
			version:      agtversion.NewParsedSemVer(1, 2, 3, "", "build19700101"),
			os:           "linux",
			arch:         "64",
			expectedName: "elastic-agent-1.2.3-linux-x86_64.tar.gz",
		},
		"snapshot build metadata is dropped": {
			version:      agtversion.NewParsedSemVer(1, 2, 3, "SNAPSHOT", "build19700101"),
			os:           "linux",
			arch:         "64",
			expectedName: "elastic-agent-1.2.3-SNAPSHOT-linux-x86_64.tar.gz",
		},
	}

	for name, test := range tests {
		t.Run(name, func(t *testing.T) {
			settings := &Config{
				OperatingSystem: test.os,
				Architecture:    test.arch,
				TargetDirectory: "/tmp/downloads",
			}

			a, err := New(test.version, settings, test.fips)
			require.NoError(t, err)
			require.Equal(t, test.expectedName, a.Filename)
			require.Equal(t, filepath.Join("/tmp/downloads", test.expectedName), a.FilePath)
		})
	}
}
