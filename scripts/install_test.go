package scripts_test

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"testing"
)

func TestUnixInstallAndUninstall(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("POSIX installer")
	}
	root, err := filepath.Abs("..")
	if err != nil {
		t.Fatal(err)
	}
	source := filepath.Join(t.TempDir(), "agentworks-source")
	if err := os.WriteFile(source, []byte("binary"), 0o755); err != nil {
		t.Fatal(err)
	}
	destination := filepath.Join(t.TempDir(), "bin")
	install := exec.Command("sh", filepath.Join(root, "scripts", "install.sh"), "--source", source, "--dir", destination)
	if output, err := install.CombinedOutput(); err != nil {
		t.Fatalf("install: %v\n%s", err, output)
	}
	installed := filepath.Join(destination, "agentworks")
	info, err := os.Stat(installed)
	if err != nil || info.Mode()&0o111 == 0 {
		t.Fatalf("installed info=%#v err=%v", info, err)
	}
	uninstall := exec.Command("sh", filepath.Join(root, "scripts", "uninstall.sh"), "--dir", destination)
	if output, err := uninstall.CombinedOutput(); err != nil {
		t.Fatalf("uninstall: %v\n%s", err, output)
	}
	if _, err := os.Stat(installed); !os.IsNotExist(err) {
		t.Fatalf("installed binary remains: %v", err)
	}
}
