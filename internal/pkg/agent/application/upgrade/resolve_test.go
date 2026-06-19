// Copyright Elasticsearch B.V. and/or licensed to Elasticsearch B.V. under one
// or more contributor license agreements. Licensed under the Elastic License 2.0;
// you may not use this file except in compliance with the Elastic License 2.0.

package upgrade

import (
	"bytes"
	"context"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/elastic/elastic-agent/internal/pkg/agent/application/upgrade/artifact"
	agtversion "github.com/elastic/elastic-agent/pkg/version"
)

func TestResolveNonDefaultSourceURI(t *testing.T) {
	version, err := agtversion.ParseVersion("8.12.0-SNAPSHOT")
	require.NoError(t, err)

	const sourceURI = "localhost:1234"
	resolved, err := resolveSnapshotSourceURI(context.TODO(), http.DefaultClient, sourceURI, version, "")
	require.NoError(t, err)
	require.Equal(t, sourceURI, resolved)
}

func readFile(t *testing.T, name string) []byte {
	b, err := os.ReadFile(name)
	require.NoError(t, err)
	return b
}

func TestResolve(t *testing.T) {
	files := map[string][]byte{
		// link to the latest snapshot build for the version
		"/latest/8.14.0-SNAPSHOT.json": readFile(t, "./testdata/latest-snapshot.json"),
	}

	tests := []struct {
		name    string
		version *agtversion.ParsedSemVer
		want    string
	}{
		{
			name:    "released version",
			version: agtversion.NewParsedSemVer(1, 2, 3, "", ""),
			want:    "https://artifacts.elastic.co/downloads/beats/elastic-agent/elastic-agent-1.2.3-linux-x86_64.tar.gz",
		},
		{
			name:    "released version with build metadata is dropped",
			version: agtversion.NewParsedSemVer(1, 2, 3, "", "build19700101"),
			want:    "https://artifacts.elastic.co/downloads/beats/elastic-agent/elastic-agent-1.2.3-linux-x86_64.tar.gz",
		},
		{
			name:    "snapshot version resolves the latest build",
			version: agtversion.NewParsedSemVer(8, 14, 0, "SNAPSHOT", ""),
			want:    "https://snapshots.elastic.co/8.14.0-6d69ee76/downloads/beats/elastic-agent/elastic-agent-8.14.0-SNAPSHOT-linux-x86_64.tar.gz",
		},
		{
			name:    "snapshot version with build metadata targets that build",
			version: agtversion.NewParsedSemVer(8, 13, 3, "SNAPSHOT", "76ce1a63"),
			want:    "https://snapshots.elastic.co/8.13.3-76ce1a63/downloads/beats/elastic-agent/elastic-agent-8.13.3-SNAPSHOT-linux-x86_64.tar.gz",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			handleDownload := func(rw http.ResponseWriter, req *http.Request) {
				file, ok := files[req.URL.Path]
				if !ok {
					rw.WriteHeader(http.StatusNotFound)
					return
				}
				_, err := io.Copy(rw, bytes.NewReader(file))
				assert.NoError(t, err, "error writing out response body")
			}
			server := httptest.NewTLSServer(http.HandlerFunc(handleDownload))
			defer server.Close()

			client := server.Client()
			transport := client.Transport.(*http.Transport)
			transport.TLSClientConfig.InsecureSkipVerify = true
			transport.DialContext = func(_ context.Context, network, _ string) (net.Conn, error) {
				return net.Dial(network, server.Listener.Addr().String())
			}

			settings := &artifact.Config{
				TargetDirectory: t.TempDir(),
			}
			a, err := artifact.New(tt.version, settings, "linux", "amd64", false)
			require.NoError(t, err)

			got, err := Resolver{}.Resolve(context.TODO(), client, a, artifact.DefaultSourceURI)
			require.NoError(t, err)
			assert.Equal(t, tt.want, got)
		})
	}
}
