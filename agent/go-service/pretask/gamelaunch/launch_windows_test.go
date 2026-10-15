package gamelaunch

import (
	"encoding/json"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"golang.org/x/sys/windows"
)

type childReceipt struct {
	Parent int
	Dir    string
	Args   []string
}

func TestMain(m *testing.M) {
	switch os.Getenv("MAAEND_LAUNCH_TEST_ROLE") {
	case "parent":
		_, _ = io.Copy(io.Discard, os.Stdin)
		return
	case "child":
		dir, err := os.Getwd()
		if err != nil {
			os.Exit(1)
		}
		data, err := json.Marshal(childReceipt{os.Getppid(), dir, os.Args[1:]})
		if err != nil {
			os.Exit(1)
		}
		if err := os.WriteFile(os.Getenv("MAAEND_LAUNCH_RECEIPT"), data, 0600); err != nil {
			os.Exit(1)
		}
		return
	default:
		os.Exit(m.Run())
	}
}

// 用无副作用的测试进程验证实际父 PID、工作目录及不附加参数，不启动游戏。
func TestNativeCreateGame(t *testing.T) {
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	receipt := filepath.Join(t.TempDir(), "receipt.json")
	parent := exec.Command(exe)
	parent.Env = append(os.Environ(), "MAAEND_LAUNCH_TEST_ROLE=parent")
	stdin, err := parent.StdinPipe()
	if err != nil {
		t.Fatal(err)
	}
	if err := parent.Start(); err != nil {
		t.Fatal(err)
	}
	defer func() { stdin.Close(); _ = parent.Wait() }()
	handle, err := windows.OpenProcess(windows.PROCESS_CREATE_PROCESS|windows.PROCESS_QUERY_LIMITED_INFORMATION, false, uint32(parent.Process.Pid))
	if err != nil {
		t.Fatal(err)
	}
	defer windows.CloseHandle(handle)
	// 游戏安装目录常含空格或中文，使用同样形式的可执行文件路径验证引号。
	childExe := filepath.Join(t.TempDir(), "无参数 game.exe")
	data, err := os.ReadFile(exe)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(childExe, data, 0700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("MAAEND_LAUNCH_RECEIPT", receipt)
	t.Setenv("MAAEND_LAUNCH_TEST_ROLE", "child")
	process, err := createGame(childExe, handle)
	if err != nil {
		t.Fatal(err)
	}
	defer windows.CloseHandle(process.Process)
	windows.CloseHandle(process.Thread)
	state, err := windows.WaitForSingleObject(process.Process, 10000)
	if err != nil || state != windows.WAIT_OBJECT_0 {
		_ = windows.TerminateProcess(process.Process, 1)
		t.Fatalf("test child did not exit: %d, %v", state, err)
	}
	data, err = os.ReadFile(receipt)
	if err != nil {
		t.Fatal(err)
	}
	var got childReceipt
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatal(err)
	}
	if got.Parent != parent.Process.Pid || !strings.EqualFold(got.Dir, filepath.Dir(childExe)) || len(got.Args) != 0 {
		t.Fatalf("unexpected native launch: %+v; expected parent %d", got, parent.Process.Pid)
	}
}

func TestRejectUntrustedSignatures(t *testing.T) {
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := verifyLauncher(exe); err == nil {
		t.Fatal("unsigned test executable accepted")
	}
	if _, err := verifyLauncher(filepath.Join(os.Getenv("SystemRoot"), "System32", "cmd.exe")); err == nil {
		t.Fatal("valid signature from a different publisher accepted")
	}
}

func TestOfficialLauncherSignature(t *testing.T) {
	path := os.Getenv("MAAEND_TEST_LAUNCHER")
	if path == "" {
		t.Skip("set MAAEND_TEST_LAUNCHER to verify an installed official launcher")
	}
	if fingerprint, err := verifyLauncher(path); err != nil {
		t.Fatal(err)
	} else {
		t.Logf("verified official launcher signer: %s", fingerprint)
	}
}

func TestRunningOfficialParent(t *testing.T) {
	if os.Getenv("MAAEND_TEST_LAUNCHER") == "" {
		t.Skip("installed official launcher check is opt-in")
	}
	handle, err := officialParent()
	if err != nil {
		t.Fatal(err)
	}
	if handle == 0 {
		t.Skip("official launcher is not running")
	}
	defer windows.CloseHandle(handle)
	pid, err := windows.GetProcessId(handle)
	if err != nil || pid == 0 {
		t.Fatalf("invalid verified parent handle: %d, %v", pid, err)
	}
	t.Logf("verified running parent PID %d", pid)
}

func TestAlreadyRunning(t *testing.T) {
	ids, err := processIDs("Endfield.exe")
	if err != nil {
		t.Fatal(err)
	}
	if len(ids) == 0 {
		t.Skip("no running game; do not launch one in tests")
	}
	if !Run([]string{"invalid config is ignored when game already runs"}) {
		t.Fatal("running game should skip all launch configuration")
	}
}
