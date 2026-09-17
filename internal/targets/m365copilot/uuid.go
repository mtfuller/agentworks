package m365copilot

import (
	"crypto/sha1"
	"fmt"
)

// dnsNamespace is the standard RFC 4122 namespace UUID for names that are
// DNS-like strings (used here to derive our own fixed namespace below).
var dnsNamespace = [16]byte{
	0x6b, 0xa7, 0xb8, 0x10, 0x9d, 0xad, 0x11, 0xd1,
	0x80, 0xb4, 0x00, 0xc0, 0x4f, 0xd4, 0x30, 0xc8,
}

// agentworksNamespace namespaces every AgentWorks-generated app id, derived
// once (at package init, not hardcoded) so it's stable across builds
// without anyone having to hand-copy a UUID into source.
var agentworksNamespace = uuidV5(dnsNamespace, "agentworks.dev")

// appID deterministically derives a Microsoft 365 app manifest `id` (must
// be a GUID) from an artifact name, so re-exporting the same artifact
// produces the same app id instead of a fresh random one every time.
func appID(artifactName string) string {
	return formatUUID(uuidV5(agentworksNamespace, artifactName))
}

// uuidV5 implements RFC 4122 section 4.3: a name-based UUID computed as a
// SHA-1 hash of a namespace UUID and a name, with the version/variant bits
// then overwritten.
func uuidV5(namespace [16]byte, name string) [16]byte {
	h := sha1.New()
	h.Write(namespace[:])
	h.Write([]byte(name))
	sum := h.Sum(nil)

	var out [16]byte
	copy(out[:], sum[:16])
	out[6] = (out[6] & 0x0F) | 0x50 // version 5
	out[8] = (out[8] & 0x3F) | 0x80 // RFC 4122 variant
	return out
}

func formatUUID(u [16]byte) string {
	return fmt.Sprintf("%x-%x-%x-%x-%x", u[0:4], u[4:6], u[6:8], u[8:10], u[10:16])
}
