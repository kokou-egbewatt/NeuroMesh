package main

// Everything that talks to Windows or WSL: wsl.exe, PowerShell (the keepalive Scheduled Task),
// nvidia-smi.exe. Command lines are built by pure functions so tests can check them.

import (
	"bytes"
	"context"
	"encoding/base64"
	"fmt"
	"os/exec"
	"strings"
	"unicode/utf16"
)

// runWSL runs a bash script as root in the distro, passing it on stdin.
func runWSL(ctx context.Context, distro, script string) (string, error) {
	cmd := exec.CommandContext(ctx, "wsl.exe", "-d", distro, "-u", "root", "--", "bash", "-s")
	cmd.Stdin = strings.NewReader(script)
	out, err := cmd.CombinedOutput()
	return decodeWSL(out), err
}

// distroState is the STATE column of `wsl -l -v` for the distro: Running, Stopped, or "" when
// it does not exist. Asking does not start the distro, unlike running anything in it.
func distroState(ctx context.Context, distro string) (string, error) {
	out, err := exec.CommandContext(ctx, "wsl.exe", "-l", "-v").Output()
	if err != nil {
		return "", fmt.Errorf("wsl -l -v: %w", err)
	}
	return parseDistroState(decodeWSL(out), distro), nil
}

func parseDistroState(list, distro string) string {
	for _, line := range strings.Split(list, "\n") {
		f := strings.Fields(strings.TrimPrefix(strings.TrimSpace(line), "*"))
		if len(f) >= 2 && f[0] == distro {
			return f[1]
		}
	}
	return ""
}

func terminateDistro(ctx context.Context, distro string) error {
	out, err := exec.CommandContext(ctx, "wsl.exe", "--terminate", distro).CombinedOutput()
	if err != nil {
		return fmt.Errorf("wsl --terminate %s: %w: %s", distro, err, decodeWSL(out))
	}
	return nil
}

// decodeWSL turns wsl.exe's own messages, which are UTF-16LE, into text. Output from programs
// inside the distro is already UTF-8 and passes through.
func decodeWSL(b []byte) string {
	if len(b) < 2 || bytes.IndexByte(b, 0) < 0 {
		return string(b)
	}
	b = bytes.TrimPrefix(b, []byte{0xff, 0xfe})
	u := make([]uint16, 0, len(b)/2)
	for i := 0; i+1 < len(b); i += 2 {
		u = append(u, uint16(b[i])|uint16(b[i+1])<<8)
	}
	return strings.ReplaceAll(string(utf16.Decode(u)), "\r", "")
}

// keepaliveCommand is what the Scheduled Task runs: a hidden PowerShell loop holding a session on
// the distro. WSL stops a distro about a minute after its last session closes, and k3s with it.
// It loops instead of relying on the task's restart-on-failure because wsl.exe exits 0 when the
// distro is terminated, so the task would count that as success and never restart.
func keepaliveCommand(distro string) (execute, args string) {
	loop := fmt.Sprintf("while ($true) { & wsl.exe -d %s --exec /bin/sleep infinity; Start-Sleep -Seconds 5 }", distro)
	return `%WINDIR%\System32\conhost.exe`,
		fmt.Sprintf(`--headless powershell.exe -NoProfile -NonInteractive -Command "%s"`, loop)
}

// psQuote quotes s as a PowerShell single-quoted string.
func psQuote(s string) string { return "'" + strings.ReplaceAll(s, "'", "''") + "'" }

// ensureKeepaliveScript registers the task when it is missing or its action differs, enables it,
// and starts it when not running. Prints ok/changed lines.
func ensureKeepaliveScript(task, distro string) string {
	exe, args := keepaliveCommand(distro)
	return fmt.Sprintf(`$ErrorActionPreference = 'Stop'
$name = %s
$exe = [Environment]::ExpandEnvironmentVariables(%s)
$argline = %s
$t = Get-ScheduledTask -TaskName $name -ErrorAction SilentlyContinue
if (-not $t -or $t.Actions[0].Execute -ne $exe -or $t.Actions[0].Arguments -ne $argline) {
  $user = "$env:USERDOMAIN\$env:USERNAME"
  $a = New-ScheduledTaskAction -Execute $exe -Argument $argline
  $tr = New-ScheduledTaskTrigger -AtLogOn -User $user
  $s = New-ScheduledTaskSettingsSet -ExecutionTimeLimit ([TimeSpan]::Zero) -AllowStartIfOnBatteries -DontStopIfGoingOnBatteries -StartWhenAvailable -MultipleInstances IgnoreNew
  $p = New-ScheduledTaskPrincipal -UserId $user -LogonType Interactive -RunLevel Limited
  if ($t) { Stop-ScheduledTask -TaskName $name }
  Register-ScheduledTask -TaskName $name -Description 'Holds a wsl.exe session so WSL does not stop the distro and the k3s cluster in it. NeuroMesh tools/cluster.' -Action $a -Trigger $tr -Settings $s -Principal $p -Force | Out-Null
  'changed keepalive task registered'
} else { 'ok keepalive task' }
$t = Get-ScheduledTask -TaskName $name
if ($t.State -eq 'Disabled') { Enable-ScheduledTask -TaskName $name | Out-Null; 'changed keepalive task enabled' }
if ((Get-ScheduledTask -TaskName $name).State -ne 'Running') { Start-ScheduledTask -TaskName $name; 'changed keepalive task started' } else { 'ok keepalive running' }
`, psQuote(task), psQuote(exe), psQuote(args))
}

func keepaliveStateScript(task string) string {
	return fmt.Sprintf(`$t = Get-ScheduledTask -TaskName %s -ErrorAction SilentlyContinue; if ($t) { [string]$t.State } else { 'missing' }`, psQuote(task))
}

func disableKeepaliveScript(task string) string {
	return fmt.Sprintf(`$name = %s
if (Get-ScheduledTask -TaskName $name -ErrorAction SilentlyContinue) {
  Stop-ScheduledTask -TaskName $name; Disable-ScheduledTask -TaskName $name | Out-Null; 'changed keepalive task stopped and disabled'
} else { 'ok no keepalive task' }
`, psQuote(task))
}

func removeKeepaliveScript(task string) string {
	return fmt.Sprintf(`$name = %s
if (Get-ScheduledTask -TaskName $name -ErrorAction SilentlyContinue) {
  Stop-ScheduledTask -TaskName $name; Unregister-ScheduledTask -TaskName $name -Confirm:$false; 'changed keepalive task removed'
} else { 'ok no keepalive task' }
`, psQuote(task))
}

// runPS runs a PowerShell script. It goes as -EncodedCommand: piped to `-Command -`, PowerShell
// reads stdin line by line and silently drops multi-line if/else blocks.
func runPS(ctx context.Context, script string) (string, error) {
	cmd := exec.CommandContext(ctx, "powershell.exe", "-NoProfile", "-NonInteractive", "-ExecutionPolicy", "Bypass",
		// progress records would otherwise reach stderr as CLIXML
		"-EncodedCommand", encodePS("$ProgressPreference = 'SilentlyContinue'\n"+script))
	out, err := cmd.CombinedOutput()
	s := strings.TrimSpace(strings.ReplaceAll(string(out), "\r", ""))
	if err != nil {
		return s, fmt.Errorf("powershell: %w: %s", err, s)
	}
	return s, nil
}

// encodePS is what -EncodedCommand takes: base64 of the script in UTF-16LE.
func encodePS(script string) string {
	u := utf16.Encode([]rune(script))
	b := make([]byte, 2*len(u))
	for i, r := range u {
		b[2*i], b[2*i+1] = byte(r), byte(r>>8)
	}
	return base64.StdEncoding.EncodeToString(b)
}

// windowsDriver is the Windows NVIDIA driver as nvidia-smi.exe reports it: layer 1.
func windowsDriver(ctx context.Context) (string, error) {
	out, err := exec.CommandContext(ctx, "nvidia-smi.exe", "--query-gpu=name,driver_version", "--format=csv,noheader").Output()
	if err != nil {
		return "", fmt.Errorf("nvidia-smi.exe: %w", err)
	}
	return strings.TrimSpace(strings.ReplaceAll(string(out), "\r", "")), nil
}
