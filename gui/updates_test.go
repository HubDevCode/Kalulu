package gui

import "testing"

func TestPickAssetFor(t *testing.T) {
	r := release{Assets: []releaseAsset{
		{Name: "Kalulu-v2.6.0-x86_64.AppImage"},
		{Name: "Kalulu-v2.6.0-x64-setup.exe"},
		{Name: "SHA256SUMS"},
	}}
	tests := []struct {
		goos, goarch string
		want         string
	}{
		{"linux", "amd64", "Kalulu-v2.6.0-x86_64.AppImage"},
		{"windows", "amd64", "Kalulu-v2.6.0-x64-setup.exe"},
		{"windows", "arm64", ""},
		{"darwin", "arm64", ""},
	}
	for _, tt := range tests {
		t.Run(tt.goos+"/"+tt.goarch, func(t *testing.T) {
			pkg, sums := pickAssetFor(r, tt.goos, tt.goarch)
			got := ""
			if pkg != nil {
				got = pkg.Name
			}
			if got != tt.want {
				t.Fatalf("package = %q, want %q", got, tt.want)
			}
			if sums == nil || sums.Name != "SHA256SUMS" {
				t.Fatalf("checksums = %v, want SHA256SUMS", sums)
			}
		})
	}
}
