package sandbox

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/google/go-containerregistry/pkg/authn"
	"github.com/google/go-containerregistry/pkg/name"
	"github.com/google/go-containerregistry/pkg/v1/remote"

	"github.com/snow-core/snow/internal/config"
)

func TestPinnedSmolVMReleaseDownloadContract(t *testing.T) {
	if os.Getenv("SNOW_TEST_SMOLVM_DOWNLOAD") != "1" {
		t.Skip("set SNOW_TEST_SMOLVM_DOWNLOAD=1 to verify the pinned release archive")
	}
	installer, ok := newOfficialInstaller().(*officialInstaller)
	if !ok {
		t.Fatal("official installer type changed")
	}
	if script, err := installer.downloadScript(context.Background()); err != nil || len(script) == 0 {
		t.Fatalf("pinned installer script: bytes=%d err=%v", len(script), err)
	}
	home, err := installerHome()
	if err != nil {
		t.Fatal(err)
	}
	release, err := installer.downloadPinnedRelease(context.Background(), home)
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(release.dir)
	if info, err := os.Stat(release.archivePath); err != nil || info.Size() == 0 {
		t.Fatalf("downloaded release: info=%v err=%v", info, err)
	}
}

func TestBuiltinProfileRegistryContract(t *testing.T) {
	if os.Getenv("SNOW_TEST_SMOLVM_PROFILE_DOWNLOAD") != "1" {
		t.Skip("set SNOW_TEST_SMOLVM_PROFILE_DOWNLOAD=1 to verify profile registry descriptors")
	}
	for _, profile := range Profiles() {
		ref, err := name.ParseReference(profile.Source, name.StrictValidation)
		if err != nil {
			t.Fatalf("profile %s: %v", profile.ID, err)
		}
		descriptor, err := remote.Get(ref, remote.WithContext(context.Background()), remote.WithAuth(authn.Anonymous))
		if err != nil {
			t.Fatalf("profile %s: %v", profile.ID, err)
		}
		if descriptor.Digest.String() != ref.Identifier() {
			t.Fatalf("profile %s digest = %s, want %s", profile.ID, descriptor.Digest, ref.Identifier())
		}
	}
}

func TestPinnedUbuntuImageDownloadContract(t *testing.T) {
	if os.Getenv("SNOW_TEST_SMOLVM_IMAGE_DOWNLOAD") != "1" {
		t.Skip("set SNOW_TEST_SMOLVM_IMAGE_DOWNLOAD=1 to verify host-side image staging")
	}
	path := filepath.Join(t.TempDir(), "ubuntu.tar")
	result, err := (registryImageFetcher{}).Fetch(context.Background(), config.DefaultUbuntuImage, path)
	if err != nil {
		t.Fatal(err)
	}
	if err := validateStagedImageArchive(path, result); err != nil {
		t.Fatal(err)
	}
	if info, err := os.Stat(path); err != nil || info.Size() == 0 {
		t.Fatalf("downloaded image archive: info=%v err=%v", info, err)
	}
}

func TestOfficialSmolVMInstallContract(t *testing.T) {
	if os.Getenv("SNOW_TEST_SMOLVM_INSTALL") != "1" {
		t.Skip("set SNOW_TEST_SMOLVM_INSTALL=1 to verify the complete pinned install")
	}
	if _, err := smolVMReleasePlatform(); err != nil {
		t.Skip(err)
	}
	home := t.TempDir()
	t.Setenv("HOME", home)
	path, err := newOfficialInstaller().Install(context.Background(), MinimumSmolVMVersion)
	if err != nil {
		t.Fatal(err)
	}
	resolvedHome, err := installerHome()
	if err != nil {
		t.Fatal(err)
	}
	want := filepath.Join(resolvedHome, ".local", "bin", "smolvm")
	if path != want {
		t.Fatalf("installed path = %q, want %q", path, want)
	}
	if _, err := validateVerifiedOfficialInstall(resolvedHome); err != nil {
		t.Fatal(err)
	}
}

// This optional contract check is skipped on ordinary CI/developer machines
// without smolvm. It pins every production flag to the external CLI rather than
// pretending an argv-accepting fake validates upstream compatibility.
func TestInstalledSmolVMCLIContract(t *testing.T) {
	path, err := exec.LookPath("smolvm")
	if err != nil {
		t.Skip("smolvm is not installed")
	}
	version, err := exec.Command(path, "--version").CombinedOutput()
	if err != nil {
		t.Fatalf("smolvm --version: %v: %s", err, version)
	}
	if err := validateSmolVMVersion(string(version)); err != nil {
		t.Fatal(err)
	}
	for _, check := range []struct {
		args  []string
		flags []string
	}{
		{[]string{"machine", "create", "--help"}, []string{"--name", "--image", "--from", "--volume", "--workdir", "--cpus", "--mem", "--storage", "--overlay", "--net", "--label"}},
		{[]string{"machine", "exec", "--help"}, []string{"--name", "--workdir", "--env", "--stream"}},
		{[]string{"machine", "start", "--help"}, []string{"--name"}},
		{[]string{"machine", "stop", "--help"}, []string{"--name"}},
		{[]string{"machine", "delete", "--help"}, []string{"--name", "--force"}},
		{[]string{"machine", "status", "--help"}, []string{"--name"}},
	} {
		out, err := exec.Command(path, check.args...).CombinedOutput()
		if err != nil {
			t.Fatalf("smolvm %s: %v: %s", strings.Join(check.args, " "), err, out)
		}
		text := string(out)
		for _, flag := range check.flags {
			if !helpHasExactLongOption(text, flag) {
				t.Errorf("smolvm %s does not advertise exact option %s", strings.Join(check.args, " "), flag)
			}
		}
	}
}

func helpHasExactLongOption(help, option string) bool {
	for _, token := range strings.Fields(help) {
		token = strings.Trim(token, "[](),")
		if token == option || strings.HasPrefix(token, option+"=") {
			return true
		}
	}
	return false
}

func TestHelpHasExactLongOption(t *testing.T) {
	if !helpHasExactLongOption("  --net  enable network", "--net") {
		t.Fatal("exact option not found")
	}
	if helpHasExactLongOption("  --network  enable network", "--net") || helpHasExactLongOption("  --memory <MIB>", "--mem") {
		t.Fatal("prefix option produced a false match")
	}
}
