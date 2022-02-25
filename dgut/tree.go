/*******************************************************************************
 * Copyright (c) 2022 Genome Research Ltd.
 *
 * Author: Sendu Bala <sb10@sanger.ac.uk>
 *
 * Permission is hereby granted, free of charge, to any person obtaining
 * a copy of this software and associated documentation files (the
 * "Software"), to deal in the Software without restriction, including
 * without limitation the rights to use, copy, modify, merge, publish,
 * distribute, sublicense, and/or sell copies of the Software, and to
 * permit persons to whom the Software is furnished to do so, subject to
 * the following conditions:
 *
 * The above copyright notice and this permission notice shall be included
 * in all copies or substantial portions of the Software.
 *
 * THE SOFTWARE IS PROVIDED "AS IS", WITHOUT WARRANTY OF ANY KIND,
 * EXPRESS OR IMPLIED, INCLUDING BUT NOT LIMITED TO THE WARRANTIES OF
 * MERCHANTABILITY, FITNESS FOR A PARTICULAR PURPOSE AND NONINFRINGEMENT.
 * IN NO EVENT SHALL THE AUTHORS OR COPYRIGHT HOLDERS BE LIABLE FOR ANY
 * CLAIM, DAMAGES OR OTHER LIABILITY, WHETHER IN AN ACTION OF CONTRACT,
 * TORT OR OTHERWISE, ARISING FROM, OUT OF OR IN CONNECTION WITH THE
 * SOFTWARE OR THE USE OR OTHER DEALINGS IN THE SOFTWARE.
 ******************************************************************************/

package dgut

import "sort"

// Tree is used to do high-level queries on DB.Store() database files.
type Tree struct {
	db *DB
}

// NewTree, given the path to a dgut database file (as created by DB.Store()),
// returns a *Tree that can be used to do high-level queries on the stats of a
// tree of disk folders.
func NewTree(path string) (*Tree, error) {
	db := NewDB(path)

	if err := db.Open(); err != nil {
		return nil, err
	}

	return &Tree{db: db}, nil
}

// DirCountSize holds nested file count and size information on a directory.
type DirCountSize struct {
	Dir   string
	Count uint64
	Size  uint64
}

// DCSs is a Size-sortable slice of DirCountSize.
type DCSs []*DirCountSize

func (d DCSs) Len() int {
	return len(d)
}
func (d DCSs) Swap(i, j int) {
	d[i], d[j] = d[j], d[i]
}
func (d DCSs) Less(i, j int) bool {
	return d[i].Size > d[j].Size
}

// DirInfo holds nested file count and size information on a directory, and also
// its immediate child directories.
type DirInfo struct {
	Current  *DirCountSize
	Children []*DirCountSize
}

// IsSameAsChild tells you if this DirInfo has only 1 child, and the child
// has the same file count. Ie. our child contains the same files as us.
func (d *DirInfo) IsSameAsChild() bool {
	return len(d.Children) == 1 && d.Children[0].Count == d.Current.Count
}

// DirInfo tells you the total number of files and their total size nested under
// the given directory. See GUTs.CountAndSize for an explanation of the filter.
//
// It also tells you the same information about the immediate child directories
// of the given directory (if the children have files in them that pass the
// filter).
//
// Returns an error if dir doesn't exist.
func (t *Tree) DirInfo(dir string, filter *Filter) (*DirInfo, error) {
	dcs, err := t.getDirCountSizeInfo(dir, filter)
	if err != nil {
		return nil, err
	}

	di := &DirInfo{
		Current: dcs,
	}

	children := t.db.Children(di.Current.Dir)
	err = t.addChildInfo(di, children, filter)

	return di, err
}

// getDirCountSizeInfo accesses the database to retrieve the count and size info
// for a given directory and filter.
func (t *Tree) getDirCountSizeInfo(dir string, filter *Filter) (*DirCountSize, error) {
	c, s, err := t.db.DirInfo(dir, filter)
	if err != nil {
		return nil, err
	}

	return &DirCountSize{
		Dir:   dir,
		Count: c,
		Size:  s,
	}, nil
}

// addChildInfo adds DirCountSize info of the given child paths to the di's
// Children. If a child dir has no files in it, it is ignored.
func (t *Tree) addChildInfo(di *DirInfo, children []string, filter *Filter) error {
	for _, child := range children {
		dcs, errc := t.getDirCountSizeInfo(child, filter)
		if errc != nil {
			return errc
		}

		if dcs.Count > 0 {
			di.Children = append(di.Children, dcs)
		}
	}

	return nil
}

// Where tells you where files are nested under dir that pass the filter. With a
// depth of 0 it only returns the single deepest directory that has all passing
// files nested under it.
//
// With a depth of 1, it also returns the results that calling Where() with a
// depth of 0 on each of the deepest directory's children would give. And so on
// recursively for higher depths.
//
// See GUTs.CountAndSize for an explanation of the filter.
//
// For example, if all user 354's files are in the directories /a/b/c/d (2
// files), /a/b/c/d/1 (1 files), /a/b/c/d/2 (2 files) and /a/b/e/f/g (2 files),
// Where("/", &Filter{UIDs: []uint32{354}}, 0) would tell you that "/a/b" has 7
// files. With a depth of 1 it would tell you that "/a/b" has 7 files,
// "/a/b/c/d" has 5 files and "/a/b/e/f/g" has 2 files. With a depth of 2 it
// would tell you that "/a/b" has 7 files, "/a/b/c/d" has 5 files, "/a/b/c/d/1"
// has 1 file, "/a/b/c/d/2" has 2 files, and "/a/b/e/f/g" has 2 files.
//
// The returned DirCountSizes are sorted by Size, largest first.
//
// Returns an error if dir doesn't exist.
func (t *Tree) Where(dir string, filter *Filter, depth int) (DCSs, error) {
	var dcss DCSs

	di, err := t.where0(dir, filter)
	if err != nil {
		return nil, err
	}

	dcss = append(dcss, di.Current)

	children := di.Children

	for i := 0; i < depth; i++ {
		var theseChildren []*DirCountSize

		for _, dcs := range children {
			// where0 can't return an error here, because we're supplying it a
			// directory name that came from the database.
			//nolint:errcheck
			diChild, _ := t.where0(dcs.Dir, filter)
			dcss = append(dcss, diChild.Current)
			theseChildren = append(theseChildren, diChild.Children...)
		}

		children = theseChildren
	}

	sort.Sort(dcss)

	return dcss, nil
}

// where0 is the implementation of Where() for a depth of 0.
func (t *Tree) where0(dir string, filter *Filter) (*DirInfo, error) {
	di, err := t.DirInfo(dir, filter)
	if err != nil {
		return nil, err
	}

	for di.IsSameAsChild() {
		// DirInfo can't return an error here, because we're supplying it a
		// directory name that came from the database.
		//nolint:errcheck
		di, _ = t.DirInfo(di.Children[0].Dir, filter)
	}

	return di, nil
}
