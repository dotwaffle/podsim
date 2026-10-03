package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
)

type syncedFile interface {
	io.Writer
	Sync() error
	Close() error
	Chmod(os.FileMode) error
	Name() string
}
type fileOps struct {
	createTemp func(string, string) (syncedFile, error)
	remove     func(string) error
	rename     func(string, string) error
	syncDir    func(string) error
}

func localFileOps() fileOps {
	return fileOps{createTemp: func(dir, pattern string) (syncedFile, error) { return os.CreateTemp(dir, pattern) }, remove: os.Remove, rename: os.Rename, syncDir: syncDirectory}
}
func syncDirectory(dir string) error {
	file, err := os.Open(dir) // #nosec G304 G703 -- The operator selects the local checkpoint directory.
	if err != nil {
		return err
	}
	defer func() { _ = file.Close() }()
	return errors.Join(file.Sync(), file.Close())
}
func regularPath(path string, mustExist bool) error {
	stat, err := os.Lstat(path)
	if errors.Is(err, os.ErrNotExist) && !mustExist {
		return nil
	}
	if err != nil {
		return err
	}
	if !stat.Mode().IsRegular() {
		return fmt.Errorf("checkpoint path %q must be a regular file", path)
	}
	return nil
}

// resolvedPath resolves existing parent links even when the file does not exist.
func resolvedPath(path string) (string, error) {
	absolute, err := filepath.Abs(path)
	if err != nil {
		return "", err
	}
	parent, err := filepath.EvalSymlinks(filepath.Dir(absolute))
	if err != nil {
		return "", fmt.Errorf("resolve output parent %q: %w", path, err)
	}
	return filepath.Join(parent, filepath.Base(absolute)), nil
}
func checkOutputPaths(opts options) error {
	sources := []string{opts.project, opts.plan, opts.checkpointInput}
	if opts.checkpointInput != "" || opts.checkpointOutput != "" {
		for _, path := range []string{opts.project, opts.plan, opts.checkpointInput, opts.output, opts.checkpointOutput} {
			if path != "" {
				if _, err := resolvedPath(path); err != nil {
					return err
				}
			}
		}
		for _, source := range sources {
			if source != "" {
				if err := regularPath(source, true); err != nil {
					return err
				}
			}
		}
		if opts.output != "" {
			if err := regularPath(opts.output, false); err != nil {
				return err
			}
		}
	}
	for _, target := range []string{opts.output, opts.checkpointOutput} {
		if target == "" {
			continue
		}
		for _, source := range sources {
			if source != "" && samePath(source, target) {
				return errors.New("output must not replace a project, plan, or checkpoint input")
			}
		}
	}
	if opts.output != "" && opts.checkpointOutput != "" && samePath(opts.output, opts.checkpointOutput) {
		return errors.New("report and checkpoint outputs must differ")
	}
	if opts.checkpointInput != "" {
		if err := regularPath(opts.checkpointInput, true); err != nil {
			return err
		}
	}
	if opts.checkpointOutput != "" {
		if err := regularPath(opts.checkpointOutput, false); err != nil {
			return err
		}
	}
	return nil
}
func publishCheckpoint(ctx context.Context, path string, protected options, encode func(io.Writer) error, ops fileOps) error {
	protected.checkpointOutput = path
	if err := checkOutputPaths(protected); err != nil {
		return err
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	// An exclusive sibling claim prevents two checkpoint writers sharing a target.
	claim := path + ".car-checkpoint-lock"
	lock, err := os.OpenFile(claim, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600) // #nosec G304 G703 -- Exclusive lock beside the operator-selected checkpoint.
	if err != nil {
		return fmt.Errorf("claim checkpoint output %q: %w", path, err)
	}
	if err = lock.Close(); err != nil {
		_ = os.Remove(claim)
		return err
	}
	defer func() { _ = os.Remove(claim) }()
	dir := filepath.Dir(path)
	file, err := ops.createTemp(dir, ".car-checkpoint-*.tmp")
	if err != nil {
		return err
	}
	defer func() { _ = file.Close(); _ = ops.remove(file.Name()) }()
	if err := file.Chmod(0o600); err != nil {
		return err
	}
	if err := encode(contextFileWriter{ctx.Err, file}); err != nil {
		return fmt.Errorf("encode checkpoint: %w", err)
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := file.Sync(); err != nil {
		return fmt.Errorf("sync checkpoint: %w", err)
	}
	if err := file.Close(); err != nil {
		return fmt.Errorf("close checkpoint: %w", err)
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := checkOutputPaths(protected); err != nil {
		return err
	}
	if err := ops.rename(file.Name(), path); err != nil {
		return fmt.Errorf("publish checkpoint: %w", err)
	}
	if err := ops.syncDir(dir); err != nil {
		return fmt.Errorf("checkpoint renamed but directory sync failed, crash durability is uncertain: %w", err)
	}
	return nil
}

type contextFileWriter struct {
	check  func() error
	writer io.Writer
}

func (w contextFileWriter) Write(data []byte) (int, error) {
	if err := w.check(); err != nil {
		return 0, err
	}
	n, err := w.writer.Write(data)
	if err == nil && n != len(data) {
		err = io.ErrShortWrite
	}
	return n, err
}
