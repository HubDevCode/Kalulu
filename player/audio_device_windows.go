//go:build windows

package player

import (
	"fmt"
	"strings"

	"github.com/bjarneo/cliamp/internal/sysproc"
)

// ListAudioDevices lists audio output devices via PowerShell on Windows.
func ListAudioDevices() ([]AudioDevice, error) {
	// Use Get-CimInstance Win32_SoundDevice for basic sound card enumeration.
	script := `Get-CimInstance Win32_SoundDevice | ForEach-Object { $_.Name + '|' + $_.DeviceID }`
	out, err := sysproc.Command("powershell", "-NoProfile", "-Command", script).Output()
	if err != nil {
		return nil, fmt.Errorf("powershell: %w", err)
	}

	var devices []AudioDevice
	for i, line := range strings.Split(strings.TrimSpace(string(out)), "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		name, id, _ := strings.Cut(line, "|")
		devices = append(devices, AudioDevice{
			Index:       i,
			Name:        strings.TrimSpace(id),
			Description: strings.TrimSpace(name),
			Active:      false,
		})
	}
	return devices, nil
}

// PrepareAudioDevice is a no-op on Windows — the system default output
// device is used. Returns a no-op cleanup.
func PrepareAudioDevice(device string) func() {
	return func() {}
}

// SwitchAudioDevice changes the Windows system default output device.
// The running audio stream keeps its original device; the change
// takes full effect on the next app restart.
func SwitchAudioDevice(deviceName string) error {
	script := fmt.Sprintf(
		`Get-AudioDevice -PlaybackCommunication | Out-Null; `+
			`Set-AudioDevice -ID '%s' -ErrorAction Stop`,
		strings.ReplaceAll(deviceName, "'", "''"),
	)
	if out, err := sysproc.Command("powershell", "-NoProfile", "-Command", script).CombinedOutput(); err != nil {
		// AudioDeviceCmdlets may not be installed; fall back to nircmd.
		cmd := sysproc.Command("nircmd", "setdefaultsounddevice", deviceName)
		if out2, err2 := cmd.CombinedOutput(); err2 != nil {
			return fmt.Errorf("failed to set output device (install AudioDeviceCmdlets or nircmd): %s / %s",
				strings.TrimSpace(string(out)), strings.TrimSpace(string(out2)))
		}
	}
	return nil
}
