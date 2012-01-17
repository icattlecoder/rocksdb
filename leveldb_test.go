package levigo

import (
	"bytes"
	"fmt"
	"os"
	"testing"
)

// This testcase is a port of leveldb's c_test.c 
func TestC(t *testing.T) {
	dbname := fmt.Sprintf("/tmp/leveldb_c_test-%d", os.Geteuid())
	// TODO: This seems impossible to do with pure Go comparators, but testing
	// that a nice API to setting the C stuff would be good.
	// cmp := NewComparator(gcmp)
	// if cmp == nil {
	// t.Errorf("NewComparator gave back something not a Comparator")
	// }
	env := NewDefaultEnv()
	cache := NewLRUCache(100000)

	options := NewOptions()
	// options.SetComparator(cmp)
	options.SetErrorIfExists(true)
	options.SetCache(cache)
	options.SetEnv(env)
	options.SetInfoLog(nil)
	options.SetWriteBufferSize(100000)
	options.SetParanoidChecks(true)
	options.SetMaxOpenFiles(10)
	options.SetBlockSize(1024)
	options.SetBlockRestartInterval(8)
	options.SetCompression(NoCompression)

	roptions := NewReadOptions()
	roptions.SetVerifyChecksums(true)
	roptions.SetFillCache(false)

	woptions := NewWriteOptions()
	woptions.SetSync(true)

	_ = DestroyDatabase(dbname, options)

	db, err := Open(dbname, options)
	if err == nil {
		t.Errorf("Open on missing db should have failed")
	}

	options.SetCreateIfMissing(true)
	db, err = Open(dbname, options)
	if err != nil {
		t.Fatalf("Open failed: %v", err)
	}

	putKey := []byte("foo")
	putValue := []byte("hello")
	err = db.Put(woptions, putKey, putValue)
	if err != nil {
		t.Errorf("Put failed: %v", err)
	}

	CheckGet(t, "after Put", db, roptions, putKey, putValue)

	wb := NewWriteBatch()
	wb.Put([]byte("foo"), []byte("a"))
	wb.Clear()
	wb.Put([]byte("bar"), []byte("b"))
	wb.Put([]byte("box"), []byte("c"))
	wb.Delete([]byte("bar"))
	err = db.Write(woptions, wb)
	if err != nil {
		t.Errorf("Write batch failed: %v", err)
	}
	CheckGet(t, "after WriteBatch", db, roptions, []byte("foo"), []byte("hello"))
	CheckGet(t, "after WriteBatch", db, roptions, []byte("bar"), nil)
	CheckGet(t, "after WriteBatch", db, roptions, []byte("box"), []byte("c"))
	// TODO: WriteBatch iteration isn't easy. Suffers same problems as
	// Comparator.
	// wbiter := &TestWBIter{t: t}
	// wb.Iterate(wbiter)
	// if wbiter.pos != 3 {
	// 	t.Errorf("After Iterate, on the wrong pos: %d", wbiter.pos)
	// }
	DestroyWriteBatch(wb)

	iter := db.Iterator(roptions)
	if iter.Valid() {
		t.Errorf("Read iterator should not be valid, yet")
	}
	iter.SeekToFirst()
	if !iter.Valid() {
		t.Errorf("Read iterator should be valid after seeking to first record")
	}
	CheckIter(t, iter, []byte("box"), []byte("c"))
	iter.Next()
	CheckIter(t, iter, []byte("foo"), []byte("hello"))
	iter.Prev()
	CheckIter(t, iter, []byte("box"), []byte("c"))
	iter.Prev()
	if iter.Valid() {
		t.Errorf("Read iterator should not be valid after go back past the first record")
	}
	iter.SeekToLast()
	CheckIter(t, iter, []byte("foo"), []byte("hello"))
	iter.Seek([]byte("b"))
	CheckIter(t, iter, []byte("box"), []byte("c"))
	if iter.GetError() != nil {
		t.Errorf("Read iterator has an error we didn't expect: %v", iter.GetError())
	}
	DestroyIterator(iter)

	// approximate sizes
	n := 20000
	woptions.SetSync(false)
	for i := 0; i < n; i++ {
		keybuf := []byte(fmt.Sprintf("k%020d", i))
		valbuf := []byte(fmt.Sprintf("v%020d", i))
		err := db.Put(woptions, keybuf, valbuf)
		if err != nil {
			t.Errorf("Put error in approximate size test: %v", err)
		}
	}

	ranges := []Range{Range{[]byte("a"), []byte("k00000000000000010000")}, Range{[]byte("k00000000000000010000"), []byte("z")}}
	sizes := db.GetApproximateSizes(ranges)
	if len(sizes) == 2 {
		if sizes[0] <= 0 {
			t.Errorf("First size range was %d", sizes[0])
		}
		if sizes[1] <= 0 {
			t.Errorf("Second size range was %d", sizes[1])
		}
	} else {
		t.Errorf("Expected 2 approx. sizes back, got %d", len(sizes))
	}

	// property
	prop := db.PropertyValue("nosuchprop")
	if prop != "" {
		t.Errorf("property nosuchprop should not have a value")
	}
	prop = db.PropertyValue("leveldb.stats")
	if prop == "" {
		t.Errorf("property leveldb.stats should have a value")
	}

	// snapshot
	snap := db.NewSnapshot()
	err = db.Delete(woptions, []byte("foo"))
	if err != nil {
		t.Errorf("Delete during snapshot test errored: %v", err)
	}
	roptions.SetSnapshot(snap)
	CheckGet(t, "from snapshot", db, roptions, []byte("foo"), []byte("hello"))
	roptions.SetSnapshot(nil)
	CheckGet(t, "from snapshot", db, roptions, []byte("foo"), nil)
	db.ReleaseSnapshot(snap)

	// repair
	db.Close()
	options.SetCreateIfMissing(false)
	options.SetErrorIfExists(false)
	err = RepairDatabase(dbname, options)
	if err != nil {
		t.Errorf("Repairing db failed: %v", err)
	}
	db, err = Open(dbname, options)
	if err != nil {
		t.Errorf("Unable to open repaired db: %v", err)
	}
	CheckGet(t, "repair", db, roptions, []byte("foo"), nil)
	CheckGet(t, "repair", db, roptions, []byte("bar"), nil)
	CheckGet(t, "repair", db, roptions, []byte("box"), []byte("c"))

	// cleanup
	db.Close()
	DestroyOptions(options)
	DestroyReadOptions(roptions)
	DestroyWriteOptions(woptions)
	DestroyCache(cache)
	// DestroyComparator(cmp)
	DestroyEnv(env)
}

func TestGetShouldReturnNilOnMiss(t *testing.T) {
	dbname := fmt.Sprintf("/tmp/leveldb_get_test-%d", os.Geteuid())
	options := NewOptions()
	options.SetErrorIfExists(true)
	options.SetCreateIfMissing(true)
	ro := NewReadOptions()
	_ = DestroyDatabase(dbname, options)
	db, err := Open(dbname, options)
	if err != nil {
		t.Fatalf("Database could not be opened: %v", err)
	}
	val, err := db.Get(ro, []byte("nope"))
	if err != nil {
		t.Errorf("Get failed: %v", err)
	}
	if val != nil {
		t.Errorf("Missing key should return nil, not %v", val)
	}
}

func CheckGet(t *testing.T, where string, db *DB, roptions *ReadOptions, key, expected []byte) {
	getValue, err := db.Get(roptions, key)

	if err != nil {
		t.Errorf("%s, Get failed: %v", where, err)
	}
	if !bytes.Equal(getValue, expected) {
		t.Errorf("%s, expected Get value %v, got %v", where, expected, getValue)
	}
}

func WBIterCheckEqual(t *testing.T, where string, which string, pos int, expected, given []byte) {
	if !bytes.Equal(expected, given) {
		t.Errorf("%s at pos %d, %s expected: %v, got: %v", where, pos, which, expected, given)
	}
}

func CheckIter(t *testing.T, it *Iterator, key, value []byte) {
	if !bytes.Equal(key, it.Key()) {
		t.Errorf("Iterator: expected key %v, got %v", key, it.Key())
	}
	if !bytes.Equal(value, it.Value()) {
		t.Errorf("Iterator: expected value %v, got %v", value, it.Value())
	}

}
