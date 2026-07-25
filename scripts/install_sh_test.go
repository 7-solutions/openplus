// Package scripts_test covers scripts/install.sh.
//
// The installer is the first thing a user runs, and until now nothing tested
// it: the only signal was a CI step that installed successfully on one
// platform. That proves the happy path and nothing about the refusals, which
// are the part that has to be right — a wrong refusal is a user who cannot
// install, and a missing refusal is a confusing loader error or a 404 instead
// of an explanation.
//
// The supported set is exactly linux/amd64, linux/arm64 and darwin/arm64
// (WSL2 reports Linux, so it needs no case of its own). Everything else must be
// refused by name. These tests drive detect_platform by putting a stub `uname`
// ahead of the real one on PATH.
package scripts_test

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// runInstaller runs install.sh with a stub uname reporting the given OS and
// machine, and returns the combined output. The installer is expected to fail
// for unsupported platforms; a supported platform gets as far as the network
// and fails there, which is why the assertions below look for the refusal text
// rather than an exit code.
func runInstaller(t *testing.T, unameS, unameM string, extraEnv ...string) string {
	t.Helper()

	bin := t.TempDir()
	stub := "#!/bin/sh\n" +
		"for a in \"$@\"; do\n" +
		"  case \"$a\" in\n" +
		"    *s*) printf '%s\\n' " + shellQuote(unameS) + "; exit 0 ;;\n" +
		"    *m*) printf '%s\\n' " + shellQuote(unameM) + "; exit 0 ;;\n" +
		"  esac\n" +
		"done\n" +
		"printf '%s\\n' " + shellQuote(unameS) + "\n"
	if err := os.WriteFile(filepath.Join(bin, "uname"), []byte(stub), 0o755); err != nil {
		t.Fatalf("write uname stub: %v", err)
	}

	// Hermetic: shadow the download tools so an accepted platform stops at the
	// first fetch instead of reaching GitHub. These tests are about the
	// platform decision, and CI must not depend on the network for it.
	for _, tool := range []string{"curl", "wget"} {
		script := "#!/bin/sh\necho 'stub: network disabled in tests' >&2\nexit 1\n"
		if err := os.WriteFile(filepath.Join(bin, tool), []byte(script), 0o755); err != nil {
			t.Fatalf("write %s stub: %v", tool, err)
		}
	}

	cmd := exec.Command("sh", "install.sh")
	cmd.Env = append(os.Environ(),
		"PATH="+bin+string(os.PathListSeparator)+os.Getenv("PATH"),
		// Keep the run offline and local: a refusal must happen before any
		// network call, and if one does not happen the test should fail on the
		// missing refusal rather than hang.
		"OPENPLUS_VERSION=v0.0.0-test",
		"OPENPLUS_INSTALL="+t.TempDir(),
	)
	cmd.Env = append(cmd.Env, extraEnv...)
	out, _ := cmd.CombinedOutput()
	return string(out)
}

func shellQuote(s string) string { return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'" }

// TestIntelMacIsRefusedByName: darwin/amd64 is not published, so the installer
// must say that rather than 404 on a darwin_amd64 archive.
func TestIntelMacIsRefusedByName(t *testing.T) {
	out := runInstaller(t, "Darwin", "x86_64")
	if !strings.Contains(out, "macOS on Intel") {
		t.Errorf("Intel macOS was not refused by name; output was:\n%s", out)
	}
	if strings.Contains(out, "darwin_amd64") {
		t.Errorf("installer tried to fetch a darwin_amd64 artifact:\n%s", out)
	}
}

// TestAppleSiliconIsAccepted: the one supported macOS platform must get past
// platform detection. It then fails on the network, which is fine — what
// matters is that no refusal fired.
func TestAppleSiliconIsAccepted(t *testing.T) {
	out := runInstaller(t, "Darwin", "arm64")
	for _, refusal := range []string{"macOS on Intel", "unsupported architecture", "unsupported operating system"} {
		if strings.Contains(out, refusal) {
			t.Errorf("darwin/arm64 was refused with %q:\n%s", refusal, out)
		}
	}
}

// TestSupportedLinuxArchesAreAccepted covers both Linux artifacts, including
// the spellings uname reports on each. WSL2 is the same path: it reports Linux.
//
// The assertion is deliberately narrow. A stubbed arch that does not match the
// host makes check_loader look for a loader that is genuinely absent (an amd64
// machine has no ld-linux-aarch64.so.1), which is correct behaviour for a real
// install and irrelevant here — so this checks only that the arch itself was
// not rejected.
func TestSupportedLinuxArchesAreAccepted(t *testing.T) {
	for _, machine := range []string{"x86_64", "amd64", "aarch64", "arm64"} {
		t.Run(machine, func(t *testing.T) {
			out := runInstaller(t, "Linux", machine)
			if strings.Contains(out, "unsupported architecture") {
				t.Errorf("linux/%s was refused:\n%s", machine, out)
			}
		})
	}
}

// TestNativeWindowsShellsAreRefused: a Linux binary cannot be installed into a
// Windows-native shell, and the fix is WSL2, so the message must name it.
func TestNativeWindowsShellsAreRefused(t *testing.T) {
	for _, sh := range []string{"MINGW64_NT-10.0", "MSYS_NT-10.0", "CYGWIN_NT-10.0"} {
		t.Run(sh, func(t *testing.T) {
			out := runInstaller(t, sh, "x86_64")
			if !strings.Contains(out, "WSL2") {
				t.Errorf("%s was not pointed at WSL2:\n%s", sh, out)
			}
		})
	}
}

// TestUnsupportedArchIsRefused: an arch with no artifact must be named, not
// left to fail as a download error.
func TestUnsupportedArchIsRefused(t *testing.T) {
	out := runInstaller(t, "Linux", "riscv64")
	if !strings.Contains(out, "unsupported architecture") {
		t.Errorf("riscv64 was not refused:\n%s", out)
	}
}

// TestUnsupportedOSIsRefused: FreeBSD and friends have no artifact.
func TestUnsupportedOSIsRefused(t *testing.T) {
	out := runInstaller(t, "FreeBSD", "x86_64")
	if !strings.Contains(out, "unsupported operating system") {
		t.Errorf("FreeBSD was not refused:\n%s", out)
	}
}

// foreignLinuxArch returns a Linux arch that is not the host's, so that
// check_loader finds no dynamic loader and reaches the NixOS branch. Testing
// that branch on a real NixOS box is not something CI can arrange, and the
// branch is the whole reason NixOS is claimed as supported — so it is exercised
// through the OPENPLUS_NIXOS_MARKER / OPENPLUS_OS_RELEASE seams instead.
func foreignLinuxArch() string {
	if runtime.GOARCH == "arm64" {
		return "x86_64"
	}
	return "aarch64"
}

// TestNixOSWithoutNixLdExplainsHowToEnableIt: the binary is FHS-linked, NixOS
// has no FHS loader, so a bare install would fail with a missing-file error
// naming a file that exists. The installer must name nix-ld and give the
// config.
func TestNixOSWithoutNixLdExplainsHowToEnableIt(t *testing.T) {
	out := runInstaller(t, "Linux", foreignLinuxArch(),
		"OPENPLUS_NIXOS_MARKER="+writeFile(t, "nixos-marker", ""),
		"NIX_LD=",
	)
	if !strings.Contains(out, "nix-ld is not enabled") {
		t.Errorf("NixOS without nix-ld was not diagnosed:\n%s", out)
	}
	if !strings.Contains(out, "programs.nix-ld.enable = true;") {
		t.Errorf("the fix was not spelled out:\n%s", out)
	}
}

// TestNixOSIsDetectedFromOsRelease: not every NixOS install has /etc/NIXOS, so
// ID=nixos in os-release must count too.
func TestNixOSIsDetectedFromOsRelease(t *testing.T) {
	out := runInstaller(t, "Linux", foreignLinuxArch(),
		"OPENPLUS_NIXOS_MARKER="+filepath.Join(t.TempDir(), "absent"),
		"OPENPLUS_OS_RELEASE="+writeFile(t, "os-release", "ID=nixos\nNAME=NixOS\n"),
		"NIX_LD=",
	)
	if !strings.Contains(out, "nix-ld is not enabled") {
		t.Errorf("NixOS was not detected from os-release:\n%s", out)
	}
}

// TestNixOSWithNixLdProceeds: with nix-ld available there is a working loader,
// so the install must continue rather than refuse.
func TestNixOSWithNixLdProceeds(t *testing.T) {
	out := runInstaller(t, "Linux", foreignLinuxArch(),
		"OPENPLUS_NIXOS_MARKER="+writeFile(t, "nixos-marker", ""),
		"NIX_LD="+writeFile(t, "ld.so", "not really a loader"),
	)
	if strings.Contains(out, "nix-ld is not enabled") {
		t.Errorf("nix-ld was present but the installer refused anyway:\n%s", out)
	}
	if !strings.Contains(out, "nix-ld") {
		t.Errorf("the nix-ld path was not taken:\n%s", out)
	}
}

// TestNonNixOSMissingLoaderSaysGlibc: a missing loader anywhere else is a glibc
// problem, and must not be misreported as a NixOS one.
func TestNonNixOSMissingLoaderSaysGlibc(t *testing.T) {
	out := runInstaller(t, "Linux", foreignLinuxArch(),
		"OPENPLUS_NIXOS_MARKER="+filepath.Join(t.TempDir(), "absent"),
		"OPENPLUS_OS_RELEASE="+writeFile(t, "os-release", "ID=debian\n"),
	)
	if !strings.Contains(out, "dynamic loader") {
		t.Errorf("a missing loader was not reported:\n%s", out)
	}
	if strings.Contains(out, "nix-ld") {
		t.Errorf("a non-NixOS system was told about nix-ld:\n%s", out)
	}
}

func writeFile(t *testing.T, name, content string) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), name)
	if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
		t.Fatalf("write %s: %v", name, err)
	}
	return p
}

// TestReleaseMatrixMatchesTheInstaller is the guard that keeps the two halves
// of the platform claim honest. The installer refusing darwin/amd64 while the
// release workflow still published it — or the reverse — is exactly the drift
// this catches.
func TestReleaseMatrixMatchesTheInstaller(t *testing.T) {
	b, err := os.ReadFile(filepath.Join("..", ".github", "workflows", "release.yml"))
	if err != nil {
		t.Fatalf("read release.yml: %v", err)
	}
	yml := string(b)

	// goos/goarch pairs appear as adjacent matrix entries; counting the arch
	// lines is enough to catch an added platform.
	if strings.Count(yml, "goos: darwin") != 1 {
		t.Errorf("release.yml builds %d darwin platforms, want exactly 1 (arm64)",
			strings.Count(yml, "goos: darwin"))
	}
	if strings.Count(yml, "goos: linux") != 2 {
		t.Errorf("release.yml builds %d linux platforms, want exactly 2 (amd64, arm64)",
			strings.Count(yml, "goos: linux"))
	}
	if strings.Contains(yml, "goos: windows") {
		t.Error("release.yml publishes a windows artifact; native Windows is not supported")
	}
}
