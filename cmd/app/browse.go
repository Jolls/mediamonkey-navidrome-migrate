package main

import (
	"bytes"
	"errors"
	"log"
	"net/http"
	"os/exec"
	"runtime"
	"strings"
)

// handleBrowseFile pops a native "open file" dialog on the machine running
// the server (which is always the user's own machine, since this app only
// listens on 127.0.0.1) and returns the chosen path. Windows-only for now.
func (s *apiServer) handleBrowseFile(w http.ResponseWriter, r *http.Request) {
	label := r.URL.Query().Get("label")
	path, err := pickFile(label)
	if err != nil {
		log.Printf("browse: %v", err)
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	log.Printf("browse: picked %q", path)
	writeJSON(w, http.StatusOK, map[string]string{"path": path})
}

func pickFile(label string) (string, error) {
	switch runtime.GOOS {
	case "windows":
		return pickFileWindows(label)
	default:
		return "", errors.New("file browse dialog is only supported on Windows")
	}
}

func pickFileWindows(label string) (string, error) {
	title := "Select file"
	if label != "" {
		title = label
	}
	script := `
Add-Type -AssemblyName System.Windows.Forms
$f = New-Object System.Windows.Forms.OpenFileDialog
$f.Title = "` + strings.ReplaceAll(title, `"`, `'`) + `"
$f.Filter = "DB files (*.db;*.DB)|*.db;*.DB|All files (*.*)|*.*"
if ($f.ShowDialog() -eq [System.Windows.Forms.DialogResult]::OK) {
  Write-Output $f.FileName
}
`
	cmd := exec.Command("powershell", "-NoProfile", "-NonInteractive", "-STA", "-Command", script)
	var out, errBuf bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &errBuf
	if err := cmd.Run(); err != nil {
		return "", errors.New("open file dialog: " + err.Error() + ": " + errBuf.String())
	}
	return strings.TrimSpace(out.String()), nil
}
