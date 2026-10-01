package vault

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"github.com/folletto/rhyolite/internal/trash"
)

// MoveResult is the outcome of a move/rename.
type MoveResult struct {
	From string `json:"from"`
	To   string `json:"to"`
}

// DeleteResult says where a deleted item went.
type DeleteResult struct {
	Path  string `json:"path"`
	Trash string `json:"trash"` // "system" or "vault" (.trash/ fallback)
}

// caseOnly reports whether dst names the same existing entry as src and
// differs only in letter case (a rename on a case-insensitive filesystem).
func (v *Vault) caseOnly(src, dst string, srcInfo fs.FileInfo) bool {
	if src == dst || !strings.EqualFold(src, dst) {
		return false
	}
	dstInfo, err := v.root.Lstat(dst)
	return err == nil && os.SameFile(srcInfo, dstInfo)
}

// rename moves src to dst, going through a temporary name for case-only renames.
func (v *Vault) rename(src, dst string, caseOnly bool) error {
	if !caseOnly {
		return v.root.Rename(src, dst)
	}
	tmp := path2(parentOf(src), fmt.Sprintf(".rhyolite-tmp-case-%d", v.now().UnixNano()))
	if err := v.root.Rename(src, tmp); err != nil {
		return err
	}
	if err := v.root.Rename(tmp, dst); err != nil {
		v.root.Rename(tmp, src) // best-effort rollback
		return err
	}
	return nil
}

// Move moves and/or renames a .md file. Missing destination folders are
// created; an existing destination is an error.
func (v *Vault) Move(src, dst string) (*MoveResult, error) {
	src, err := CleanFile(src)
	if err != nil {
		return nil, err
	}
	dst, err = CleanFile(dst)
	if err != nil {
		return nil, err
	}
	v.mu.Lock()
	defer v.mu.Unlock()
	if err := v.move(src, dst, false); err != nil {
		return nil, v.sanitize(src, err)
	}
	return &MoveResult{From: src, To: dst}, nil
}

// FolderMove moves and/or renames a folder.
func (v *Vault) FolderMove(src, dst string) (*MoveResult, error) {
	src, err := cleanFolderNotRoot(src)
	if err != nil {
		return nil, err
	}
	dst, err = cleanFolderNotRoot(dst)
	if err != nil {
		return nil, err
	}
	if isWithin(strings.ToLower(dst), strings.ToLower(src)) && !strings.EqualFold(src, dst) {
		return nil, errf("cannot move a folder into itself")
	}
	v.mu.Lock()
	defer v.mu.Unlock()
	if err := v.move(src, dst, true); err != nil {
		return nil, v.sanitize(src, err)
	}
	return &MoveResult{From: src, To: dst}, nil
}

func (v *Vault) move(src, dst string, folder bool) error {
	if src == dst {
		return errf("source and destination are the same")
	}
	info, err := v.lstat(src)
	if err != nil {
		return err
	}
	if folder != info.IsDir() || (!folder && !info.Mode().IsRegular()) {
		return errNotFound(src)
	}
	if err := v.checkParents(dst); err != nil {
		return err
	}
	co := v.caseOnly(src, dst, info)
	if !co {
		if _, err := v.root.Lstat(dst); err == nil {
			return errf("destination already exists: %q", dst)
		}
		if err := v.mkParents(dst); err != nil {
			return err
		}
	}
	return v.rename(src, dst, co)
}

// FolderCreate creates a folder and any missing parents (like mkdir -p).
func (v *Vault) FolderCreate(rel string) (string, error) {
	rel, err := cleanFolderNotRoot(rel)
	if err != nil {
		return "", err
	}
	v.mu.Lock()
	defer v.mu.Unlock()
	if err := v.checkParents(path2(rel, "x")); err != nil {
		return "", v.sanitize(rel, err)
	}
	if err := v.root.MkdirAll(rel, 0o755); err != nil {
		return "", v.sanitize(rel, err)
	}
	return rel, nil
}

// Delete moves a file to the trash.
func (v *Vault) Delete(rel string) (*DeleteResult, error) {
	rel, err := CleanFile(rel)
	if err != nil {
		return nil, err
	}
	v.mu.Lock()
	defer v.mu.Unlock()
	if _, err := v.existingFile(rel); err != nil {
		return nil, v.sanitize(rel, err)
	}
	where, err := v.trashItem(rel)
	if err != nil {
		return nil, v.sanitize(rel, err)
	}
	return &DeleteResult{Path: rel, Trash: where}, nil
}

// FolderDelete trashes a folder that has no visible content (no .md files
// and no subfolders). Hidden or non-markdown leftovers go to the trash with it.
func (v *Vault) FolderDelete(rel string) (*DeleteResult, error) {
	rel, err := cleanFolderNotRoot(rel)
	if err != nil {
		return nil, err
	}
	v.mu.Lock()
	defer v.mu.Unlock()
	info, err := v.lstat(rel)
	if err != nil {
		return nil, v.sanitize(rel, err)
	}
	if !info.IsDir() {
		return nil, errNotFound(rel)
	}
	entries, err := v.visible(rel)
	if err != nil {
		return nil, v.sanitize(rel, err)
	}
	if len(entries) > 0 {
		return nil, errf("folder is not empty: %q has %d item(s); only empty folders can be deleted", rel, len(entries))
	}
	where, err := v.trashItem(rel)
	if err != nil {
		return nil, v.sanitize(rel, err)
	}
	return &DeleteResult{Path: rel, Trash: where}, nil
}

// trashItem sends rel to the system trash, or to <vault>/.trash/ only when
// the system reports it has no trash for this volume. Any other failure
// leaves the item where it is.
func (v *Vault) trashItem(rel string) (string, error) {
	abs := filepath.Join(v.abs, filepath.FromSlash(rel))
	// Last check before leaving the vault's root handle: the path must still
	// resolve to itself, i.e. no symlink swapped in along the way.
	if real, err := filepath.EvalSymlinks(abs); err != nil || real != abs {
		return "", errNotFound(rel)
	}
	err := v.trash.Trash(abs)
	if err == nil {
		return "system", nil
	}
	if !errors.Is(err, trash.ErrUnavailable) {
		return "", errf("could not move %q to the trash", rel)
	}
	dst := ".trash/" + rel
	for i := 0; ; i++ {
		cand := dst
		if i > 0 {
			cand = suffixed(dst, v.now().Format("20060102-150405"), i)
		}
		if _, err := v.root.Lstat(cand); err == nil {
			continue
		}
		dst = cand
		break
	}
	if err := v.root.MkdirAll(parentOf(dst), 0o755); err != nil {
		return "", err
	}
	if err := v.root.Rename(rel, dst); err != nil {
		return "", err
	}
	return "vault", nil
}

// suffixed turns "a/b.md" into "a/b.<stamp>.md" (", stamp-2" on repeats).
func suffixed(p, stamp string, n int) string {
	if n > 1 {
		stamp = fmt.Sprintf("%s-%d", stamp, n)
	}
	ext := ""
	base := p
	if i := strings.LastIndexByte(p, '.'); i > strings.LastIndexByte(p, '/')+1 {
		base, ext = p[:i], p[i:]
	}
	return base + "." + stamp + ext
}
