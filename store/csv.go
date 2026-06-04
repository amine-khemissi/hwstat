// Package store handles the long-term time-series persistence: one append-only
// CSV file per dimension, with daily rotation. Same on-disk format as nstat so
// the two tools' data is interchangeable for tooling.
package store

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

// Append writes one row to the CSV file for the given dimension. Creates the
// file with a header if it does not exist.
func Append(path, dimName string, t time.Time, value float64) error {
	needHeader := false
	if _, err := os.Stat(path); os.IsNotExist(err) {
		needHeader = true
	}

	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		return err
	}
	defer f.Close()

	if needHeader {
		if _, err := fmt.Fprintln(f, "dimension,timestamp,value"); err != nil {
			return err
		}
	}

	_, err = fmt.Fprintf(f, "%s,%s,%.4f\n",
		dimName,
		t.Format("2006-01-02 15:04:05"),
		value,
	)
	return err
}

// Point is one sample read back from a CSV time-series.
type Point struct {
	T time.Time
	V float64
}

// Load reads a dimension CSV (and its rotated .1/.2/.3 backups) and returns the
// points newer than `since`. A zero `since` returns everything. Points are
// returned oldest-first.
func Load(path string, since time.Time) ([]Point, error) {
	// Read oldest backups first so the merged series is chronological.
	files := []string{path + ".3", path + ".2", path + ".1", path}
	var pts []Point
	for _, fp := range files {
		f, err := os.Open(fp)
		if err != nil {
			continue // missing backup is fine
		}
		sc := bufio.NewScanner(f)
		for sc.Scan() {
			line := sc.Text()
			if line == "" || strings.HasPrefix(line, "dimension,") {
				continue
			}
			parts := strings.SplitN(line, ",", 3)
			if len(parts) != 3 {
				continue
			}
			t, err := time.ParseInLocation("2006-01-02 15:04:05", parts[1], time.Local)
			if err != nil {
				continue
			}
			if !since.IsZero() && t.Before(since) {
				continue
			}
			v, err := strconv.ParseFloat(parts[2], 64)
			if err != nil {
				continue
			}
			pts = append(pts, Point{T: t, V: v})
		}
		f.Close()
	}
	return pts, nil
}

// RotateCSVs rotates all CSV files in the given directory.
// csv_*.csv -> csv_*.csv.1 -> csv_*.csv.2 -> csv_*.csv.3 (deleted)
func RotateCSVs(dir string) error {
	pattern := filepath.Join(dir, "csv_*.csv")
	files, err := filepath.Glob(pattern)
	if err != nil {
		return err
	}
	for _, f := range files {
		rotateFile(f, 3)
	}
	return nil
}

// rotateFile rotates a single file up to maxBackups copies.
func rotateFile(path string, maxBackups int) {
	oldest := fmt.Sprintf("%s.%d", path, maxBackups)
	os.Remove(oldest)
	for i := maxBackups - 1; i >= 1; i-- {
		src := fmt.Sprintf("%s.%d", path, i)
		dst := fmt.Sprintf("%s.%d", path, i+1)
		os.Rename(src, dst)
	}
	if _, err := os.Stat(path); err == nil {
		os.Rename(path, path+".1")
	}
}
