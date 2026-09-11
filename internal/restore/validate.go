package restore

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

// ValidationResult contains the details of a backup file inspection
type ValidationResult struct {
	FilePath       string   `json:"file_path"`
	FileName       string   `json:"file_name"`
	SizeBytes      int64    `json:"size_bytes"`
	SizeMB         float64  `json:"size_mb"`
	LineCount      int      `json:"line_count"`
	DetectedType   string   `json:"detected_type"` // "postgres", "mysql", "unknown"
	IsValid        bool     `json:"is_valid"`
	HeaderInfo     string   `json:"header_info"`
	TableCount     int      `json:"table_count"`
	StatementCount int      `json:"statement_count"`
	Errors         []string `json:"errors"`
	Warnings       []string `json:"warnings"`
	TablesFound    []string `json:"tables_found"`
}

var (
	postgresHeaderRegex = regexp.Regexp{}
	mysqlHeaderRegex    = regexp.Regexp{}
	createTableRegex    = regexp.MustCompile(`(?i)CREATE\s+TABLE\s+(?:IF\s+NOT\s+EXISTS\s+)?(?:["'` + "`" + `]?([a-zA-Z0-9_]+)["'` + "`" + `]?\.)?["'` + "`" + `]?([a-zA-Z0-9_]+)["'` + "`" + `]?`)
)

// ValidateBackupFile validates and inspects a SQL backup file
func ValidateBackupFile(filePath string, expectedType string) (*ValidationResult, error) {
	if filePath == "" {
		return nil, fmt.Errorf("backup file path is required")
	}

	absPath, err := filepath.Abs(filePath)
	if err != nil {
		absPath = filePath
	}

	result := &ValidationResult{
		FilePath:     absPath,
		FileName:     filepath.Base(absPath),
		DetectedType: "unknown",
		IsValid:      true,
		Errors:       []string{},
		Warnings:     []string{},
		TablesFound:  []string{},
	}

	// 1. File existence check
	fileInfo, err := os.Stat(absPath)
	if err != nil {
		if os.IsNotExist(err) {
			result.IsValid = false
			result.Errors = append(result.Errors, fmt.Sprintf("file does not exist: %s", absPath))
			return result, nil
		}
		return nil, fmt.Errorf("failed to access backup file: %w", err)
	}

	if fileInfo.IsDir() {
		result.IsValid = false
		result.Errors = append(result.Errors, fmt.Sprintf("specified path is a directory, not a file: %s", absPath))
		return result, nil
	}

	result.SizeBytes = fileInfo.Size()
	result.SizeMB = float64(result.SizeBytes) / (1024 * 1024)

	// Empty file check
	if result.SizeBytes == 0 {
		result.IsValid = false
		result.Errors = append(result.Errors, "backup file is empty (0 bytes)")
		return result, nil
	}

	// 2. Open and scan file content
	file, err := os.Open(absPath)
	if err != nil {
		result.IsValid = false
		result.Errors = append(result.Errors, fmt.Sprintf("cannot open file for reading: %v", err))
		return result, nil
	}
	defer file.Close()

	scanner := bufio.NewScanner(file)
	// Buffer size up to 1MB lines
	buf := make([]byte, 64*1024)
	scanner.Buffer(buf, 1024*1024)

	lineNum := 0
	pgMarkers := 0
	mysqlMarkers := 0
	inHeader := true
	headerLines := []string{}
	tableMap := make(map[string]bool)

	for scanner.Scan() {
		lineNum++
		line := scanner.Text()
		trimmed := strings.TrimSpace(line)

		if lineNum <= 20 && trimmed != "" {
			if inHeader && (strings.HasPrefix(trimmed, "--") || strings.HasPrefix(trimmed, "/*")) {
				headerLines = append(headerLines, trimmed)
			} else if !strings.HasPrefix(trimmed, "--") && !strings.HasPrefix(trimmed, "/*") {
				inHeader = false
			}
		}

		// Detect DB markers
		lowerLine := strings.ToLower(line)
		if strings.Contains(lowerLine, "postgresql database dump") ||
			strings.Contains(lowerLine, "pg_dump") ||
			strings.Contains(lowerLine, "set search_path") ||
			strings.Contains(lowerLine, "copy ") {
			pgMarkers++
		}

		if strings.Contains(lowerLine, "mysql dump") ||
			strings.Contains(lowerLine, "mariadb dump") ||
			strings.Contains(lowerLine, "mysqldump") ||
			strings.Contains(lowerLine, "set foreign_key_checks") ||
			strings.Contains(lowerLine, "engine=") {
			mysqlMarkers++
		}

		// Statement counting
		if strings.HasSuffix(trimmed, ";") {
			result.StatementCount++
		}

		// Table extraction
		if matches := createTableRegex.FindStringSubmatch(line); len(matches) > 0 {
			tableName := matches[len(matches)-1]
			if tableName != "" && !tableMap[tableName] {
				tableMap[tableName] = true
				result.TablesFound = append(result.TablesFound, tableName)
			}
		}

		// Check for potentially destructive commands
		if strings.HasPrefix(strings.ToUpper(trimmed), "DROP DATABASE") {
			result.Warnings = append(result.Warnings, fmt.Sprintf("line %d: contains DROP DATABASE statement", lineNum))
		}
	}

	if err := scanner.Err(); err != nil {
		// If line was too long or scanning failed midway
		result.Warnings = append(result.Warnings, fmt.Sprintf("warning while reading file: %v", err))
	}

	result.LineCount = lineNum
	result.TableCount = len(result.TablesFound)

	// Set header summary
	if len(headerLines) > 0 {
		result.HeaderInfo = strings.Join(headerLines, " | ")
	}

	// 3. Determine database type
	if pgMarkers > mysqlMarkers {
		result.DetectedType = "postgres"
	} else if mysqlMarkers > pgMarkers {
		result.DetectedType = "mysql"
	} else {
		// Fallback detection from file extension or content keywords
		if strings.HasSuffix(strings.ToLower(absPath), ".pgsql") || strings.HasSuffix(strings.ToLower(absPath), ".postgres") {
			result.DetectedType = "postgres"
		} else if strings.HasSuffix(strings.ToLower(absPath), ".mysql") {
			result.DetectedType = "mysql"
		}
	}

	// 4. Validate against expected type if specified
	if expectedType != "" && expectedType != "unknown" {
		if result.DetectedType != "unknown" && result.DetectedType != expectedType {
			result.Warnings = append(result.Warnings, fmt.Sprintf("detected database type (%s) differs from target database type (%s)", result.DetectedType, expectedType))
		}
	}

	// Minimum validity requirements
	if result.StatementCount == 0 && result.LineCount > 10 {
		result.Warnings = append(result.Warnings, "no standard semicolon-terminated SQL statements detected in file")
	}

	if len(result.Errors) > 0 {
		result.IsValid = false
	}

	return result, nil
}

// FormatValidationReport returns a human-readable summary of validation results
func FormatValidationReport(res *ValidationResult) string {
	var sb strings.Builder
	sb.WriteString("Backup Validation Report:\n")
	sb.WriteString(strings.Repeat("=", 50) + "\n")
	sb.WriteString(fmt.Sprintf("  File Name:       %s\n", res.FileName))
	sb.WriteString(fmt.Sprintf("  File Path:       %s\n", res.FilePath))
	sb.WriteString(fmt.Sprintf("  File Size:       %.2f MB (%d bytes)\n", res.SizeMB, res.SizeBytes))
	sb.WriteString(fmt.Sprintf("  Line Count:      %d\n", res.LineCount))
	sb.WriteString(fmt.Sprintf("  Detected Type:   %s\n", res.DetectedType))
	sb.WriteString(fmt.Sprintf("  Tables Found:    %d\n", res.TableCount))

	if res.HeaderInfo != "" {
		sb.WriteString(fmt.Sprintf("  Header Summary:  %s\n", res.HeaderInfo))
	}

	statusStr := "VALID"
	if !res.IsValid {
		statusStr = "INVALID"
	}
	sb.WriteString(fmt.Sprintf("  Validation:      %s\n", statusStr))

	if len(res.TablesFound) > 0 {
		sb.WriteString("\n  Detected Tables:\n")
		limit := 10
		if len(res.TablesFound) < limit {
			limit = len(res.TablesFound)
		}
		for i := 0; i < limit; i++ {
			sb.WriteString(fmt.Sprintf("    - %s\n", res.TablesFound[i]))
		}
		if len(res.TablesFound) > limit {
			sb.WriteString(fmt.Sprintf("    ... and %d more\n", len(res.TablesFound)-limit))
		}
	}

	if len(res.Warnings) > 0 {
		sb.WriteString("\n  Warnings:\n")
		for _, w := range res.Warnings {
			sb.WriteString(fmt.Sprintf("    [!] %s\n", w))
		}
	}

	if len(res.Errors) > 0 {
		sb.WriteString("\n  Errors:\n")
		for _, e := range res.Errors {
			sb.WriteString(fmt.Sprintf("    [X] %s\n", e))
		}
	}

	sb.WriteString(strings.Repeat("=", 50))
	return sb.String()
}
