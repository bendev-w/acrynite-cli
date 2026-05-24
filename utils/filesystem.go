// Filesystem utilities for managing files and directories
package utils

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// Exists checks if a file or directory exists
func Exists(path string) bool {
	_, err := os.Stat(path)
	return !os.IsNotExist(err)
}

// IsDir checks if the path is a directory
func IsDir(path string) bool {
	info, err := os.Stat(path)
	if err != nil {
		return false
	}
	return info.IsDir()
}

// IsFile checks if the path is a file
func IsFile(path string) bool {
	info, err := os.Stat(path)
	if err != nil {
		return false
	}
	return !info.IsDir()
}

// CreateDir creates a directory and all parent directories if needed
func CreateDir(path string) error {
	return os.MkdirAll(path, 0755)
}

// CreateFile creates a new file with the specified content
func CreateFile(path string, content []byte) error {
	// Ensure parent directory exists
	dir := filepath.Dir(path)
	if err := CreateDir(dir); err != nil {
		return fmt.Errorf("failed to create parent directory: %w", err)
	}
	return os.WriteFile(path, content, 0644)
}

// ReadFile reads the content of a file
func ReadFile(path string) ([]byte, error) {
	return os.ReadFile(path)
}

// WriteFile writes content to a file, creating it if it doesn't exist
func WriteFile(path string, content []byte) error {
	// Ensure parent directory exists
	dir := filepath.Dir(path)
	if err := CreateDir(dir); err != nil {
		return fmt.Errorf("failed to create parent directory: %w", err)
	}
	return os.WriteFile(path, content, 0644)
}

// AppendToFile appends content to a file
func AppendToFile(path string, content []byte) error {
	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		return err
	}
	defer f.Close()
	
	_, err = f.Write(content)
	return err
}

// CopyFile copies a file from src to dst
func CopyFile(src, dst string) error {
	srcFile, err := os.Open(src)
	if err != nil {
		return err
	}
	defer srcFile.Close()

	// Ensure destination directory exists
	dstDir := filepath.Dir(dst)
	if err := CreateDir(dstDir); err != nil {
		return err
	}

	dstFile, err := os.Create(dst)
	if err != nil {
		return err
	}
	defer dstFile.Close()

	_, err = io.Copy(dstFile, srcFile)
	return err
}

// MoveFile moves a file from src to dst
func MoveFile(src, dst string) error {
	// Ensure destination directory exists
	dstDir := filepath.Dir(dst)
	if err := CreateDir(dstDir); err != nil {
		return err
	}
	
	return os.Rename(src, dst)
}

// DeleteFile deletes a file
func DeleteFile(path string) error {
	return os.Remove(path)
}

// DeleteDir deletes a directory and all its contents
func DeleteDir(path string) error {
	return os.RemoveAll(path)
}

// ListFiles returns all files in a directory (non-recursive)
func ListFiles(dir string) ([]string, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}

	var files []string
	for _, entry := range entries {
		if !entry.IsDir() {
			files = append(files, filepath.Join(dir, entry.Name()))
		}
	}
	return files, nil
}

// ListDirs returns all subdirectories in a directory (non-recursive)
func ListDirs(dir string) ([]string, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}

	var dirs []string
	for _, entry := range entries {
		if entry.IsDir() {
			dirs = append(dirs, filepath.Join(dir, entry.Name()))
		}
	}
	return dirs, nil
}

// WalkDir walks a directory tree and calls the function for each file
func WalkDir(root string, fn func(path string, info os.FileInfo) error) error {
	return filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		return fn(path, info)
	})
}

// FindFiles finds files matching a pattern using filepath.Glob
func FindFiles(root, pattern string) ([]string, error) {
	var matches []string
	err := filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if info.IsDir() {
			return nil
		}
		matched, err := filepath.Match(pattern, filepath.Base(path))
		if err != nil {
			return err
		}
		if matched {
			matches = append(matches, path)
		}
		return nil
	})
	return matches, err
}

// BackupFile creates a backup of a file with timestamp
func BackupFile(path string) (string, error) {
	if !Exists(path) {
		return "", fmt.Errorf("file does not exist: %s", path)
	}

	timestamp := Now(TimestampBackup)
	ext := filepath.Ext(path)
	base := strings.TrimSuffix(path, ext)
	backupPath := fmt.Sprintf("%s_%s%s", base, timestamp, ext)

	if err := CopyFile(path, backupPath); err != nil {
		return "", fmt.Errorf("failed to create backup: %w", err)
	}

	return backupPath, nil
}

// BackupFileToDir creates a backup of a file in a specific directory
func BackupFileToDir(path, backupDir string) (string, error) {
	if !Exists(path) {
		return "", fmt.Errorf("file does not exist: %s", path)
	}

	// Ensure backup directory exists
	if err := CreateDir(backupDir); err != nil {
		return "", err
	}

	timestamp := Now(TimestampBackup)
	filename := filepath.Base(path)
	ext := filepath.Ext(filename)
	base := strings.TrimSuffix(filename, ext)
	backupName := fmt.Sprintf("%s_%s%s", base, timestamp, ext)
	backupPath := filepath.Join(backupDir, backupName)

	if err := CopyFile(path, backupPath); err != nil {
		return "", fmt.Errorf("failed to create backup: %w", err)
	}

	return backupPath, nil
}

// CleanOldBackups removes backup files older than maxAge
func CleanOldBackups(dir, pattern string, maxAge time.Duration) error {
	files, err := FindFiles(dir, pattern)
	if err != nil {
		return err
	}

	cutoff := time.Now().Add(-maxAge)
	for _, file := range files {
		info, err := os.Stat(file)
		if err != nil {
			continue
		}
		if info.ModTime().Before(cutoff) {
			if err := DeleteFile(file); err != nil {
				Warning("Failed to delete old backup %s: %v", file, err)
			} else {
				Info("Deleted old backup: %s", file)
			}
		}
	}
	return nil
}

// GetFileSize returns the size of a file in bytes
func GetFileSize(path string) (int64, error) {
	info, err := os.Stat(path)
	if err != nil {
		return 0, err
	}
	return info.Size(), nil
}

// GetFileModTime returns the modification time of a file
func GetFileModTime(path string) (time.Time, error) {
	info, err := os.Stat(path)
	if err != nil {
		return time.Time{}, err
	}
	return info.ModTime(), nil
}

// ExpandPath expands ~ to home directory in paths
func ExpandPath(path string) (string, error) {
	if !strings.HasPrefix(path, "~") {
		return path, nil
	}

	home, err := GetHomeDir()
	if err != nil {
		return "", err
	}

	if path == "~" {
		return home, nil
	}

	return filepath.Join(home, path[2:]), nil
}

// GetHomeDir returns the user's home directory
func GetHomeDir() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("failed to get home directory: %w", err)
	}
	return home, nil
}

// EnsureDirExists creates a directory if it doesn't exist
func EnsureDirExists(path string) error {
	if Exists(path) {
		if !IsDir(path) {
			return fmt.Errorf("path exists but is not a directory: %s", path)
		}
		return nil
	}
	return CreateDir(path)
}


