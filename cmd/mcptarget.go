package cmd

import (
	"os"

	"github.com/mtfuller/agentworks/internal/artifact"
	"github.com/mtfuller/agentworks/internal/mcpclient"
	"github.com/mtfuller/agentworks/internal/targets/mcpconfig"
)

// mcpTarget describes how to reach an mcp artifact's server: a local process
// (command, args, and env run from the artifact's directory) or a remote
// http/sse endpoint. For a remote server, ${VAR} references in its headers are
// expanded from the real environment, and missing names the variables that
// were unset or empty -- a header that references one is left out rather than
// sent half-expanded, so the server will answer 401 and the caller can say why.
func mcpTarget(a *artifact.Artifact) (target mcpclient.Target, missing []string) {
	if mcpconfig.IsRemote(a) {
		headers, missing := mcpclient.ExpandEnv(a.ExtraStringMap("headers"), mcpclient.LookupOSEnv)
		return mcpclient.Target{
			Transport: mcpconfig.TransportOf(a),
			URL:       a.ExtraString("url"),
			Headers:   headers,
		}, missing
	}
	return mcpclient.Target{
		Transport: mcpclient.TransportStdio,
		Command:   mcpconfig.CommandLine(a),
		Dir:       a.Dir,
		Env:       append(os.Environ(), mcpEnvPairs(a)...),
	}, nil
}
