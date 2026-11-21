package env

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestRenderEtcEnvironmentReplacesOwnBlock(t *testing.T) {
	current := "PATH=\"/usr/bin\"\nLANG=C\n" + blockBegin + "\nOLD=\"gone\"\n" + blockEnd + "\n"
	got, skipped := RenderEtcEnvironment(current, map[string]string{"B": "2", "A": "1"})
	assert.Empty(t, skipped)
	assert.Equal(t, "PATH=\"/usr/bin\"\nLANG=C\n"+blockBegin+"\nA=\"1\"\nB=\"2\"\n"+blockEnd+"\n", got,
		"user lines kept, block replaced and sorted")

	again, _ := RenderEtcEnvironment(got, map[string]string{"A": "1", "B": "2"})
	assert.Equal(t, got, again, "idempotent")

	removed, _ := RenderEtcEnvironment(got, nil)
	assert.Equal(t, "PATH=\"/usr/bin\"\nLANG=C\n", removed, "an empty env removes the block")
	empty, _ := RenderEtcEnvironment("", nil)
	assert.Equal(t, "", empty)
}

// Values are data, never shell: a value that tried to end a heredoc or
// inject a command used to run as root.
func TestRenderEtcEnvironmentRejectsUnrepresentableValues(t *testing.T) {
	got, skipped := RenderEtcEnvironment("", map[string]string{
		"OK":        `a'b c $(id) ; rm -rf /`,
		"QUOTE":     `a"b`,
		"BACKSLASH": `a\b`,
		"MULTILINE": "x\nDCXEOF\ntouch /pwned",
		"BAD-NAME":  "v",
		"1LEADING":  "v",
	})
	assert.ElementsMatch(t, []string{"QUOTE", "BACKSLASH", "MULTILINE", "BAD-NAME", "1LEADING"}, skipped)
	assert.Equal(t, blockBegin+"\nOK=\"a'b c $(id) ; rm -rf /\"\n"+blockEnd+"\n", got)
}

// The /etc/profile patch keeps an inherited PATH, as the sed expression it
// replaces did: only an assignment without a $ that ends its line changes.
func TestPatchProfilePATH(t *testing.T) {
	profile := "if [ \"$(id -u)\" -eq 0 ]; then\n" +
		"  PATH=\"/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin\"\n" +
		"else\n" +
		"PATH=/usr/local/bin:/usr/bin:/bin\n" +
		"fi\n" +
		"export PATH\n" +
		"PATH=$PATH:/opt/bin\n" +
		"MANPATH=/usr/share/man\n"
	want := "if [ \"$(id -u)\" -eq 0 ]; then\n" +
		"  PATH=${PATH:-\"/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin\"}\n" +
		"else\n" +
		"PATH=${PATH:-/usr/local/bin:/usr/bin:/bin}\n" +
		"fi\n" +
		"export PATH\n" +
		"PATH=$PATH:/opt/bin\n" +
		"MANPATH=/usr/share/man\n"
	assert.Equal(t, want, PatchProfilePATH(profile))
}
