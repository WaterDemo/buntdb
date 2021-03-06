package buntdb

import (
	"bufio"
	"bytes"
	"errors"
	"fmt"
	"io/ioutil"
	"math/rand"
	"os"
	"strconv"
	"strings"
	"testing"
	"time"
)

func TestBackgroudOperations(t *testing.T) {
	if err := os.RemoveAll("data.db"); err != nil {
		t.Fatal(err)
	}
	db, err := Open("data.db")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = os.RemoveAll("data.db") }()
	defer func() { _ = db.Close() }()
	for i := 0; i < 1000; i++ {
		if err := db.Update(func(tx *Tx) error {
			for j := 0; j < 200; j++ {
				if _, _, err := tx.Set(fmt.Sprintf("hello%d", j), "planet", nil); err != nil {
					return err
				}
			}
			if _, _, err := tx.Set("hi", "world", &SetOptions{Expires: true, TTL: time.Second / 2}); err != nil {
				return err
			}
			return nil
		}); err != nil {
			t.Fatal(err)
		}
	}
	n := 0
	err = db.View(func(tx *Tx) error {
		var err error
		n, err = tx.Len()
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
	if n != 201 {
		t.Fatalf("expecting '%v', got '%v'", 201, n)
	}
	time.Sleep(time.Millisecond * 1500)
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	db, err = Open("data.db")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = os.RemoveAll("data.db") }()
	defer func() { _ = db.Close() }()
	n = 0
	err = db.View(func(tx *Tx) error {
		var err error
		n, err = tx.Len()
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
	if n != 200 {
		t.Fatalf("expecting '%v', got '%v'", 200, n)
	}
}
func TestVariousTx(t *testing.T) {
	if err := os.RemoveAll("data.db"); err != nil {
		t.Fatal(err)
	}
	db, err := Open("data.db")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = os.RemoveAll("data.db") }()
	defer func() { _ = db.Close() }()
	if err := db.Update(func(tx *Tx) error {
		_, _, err := tx.Set("hello", "planet", nil)
		return err
	}); err != nil {
		t.Fatal(err)
	}
	errBroken := errors.New("broken")
	if err := db.Update(func(tx *Tx) error {
		_, _, _ = tx.Set("hello", "world", nil)
		return errBroken
	}); err != errBroken {
		t.Fatalf("did not correctly receive the user-defined transaction error.")
	}
	var val string
	err = db.View(func(tx *Tx) error {
		var err error
		val, err = tx.Get("hello")
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
	if val == "world" {
		t.Fatal("a rollbacked transaction got through")
	}
	if val != "planet" {
		t.Fatalf("expecting '%v', got '%v'", "planet", val)
	}
	if err := db.Update(func(tx *Tx) error {
		tx.db = nil
		if _, _, err := tx.Set("hello", "planet", nil); err != ErrTxClosed {
			t.Fatal("expecting a tx closed error")
		}
		if _, err := tx.Delete("hello"); err != ErrTxClosed {
			t.Fatal("expecting a tx closed error")
		}
		if _, err := tx.Get("hello"); err != ErrTxClosed {
			t.Fatal("expecting a tx closed error")
		}
		tx.db = db
		tx.writable = false
		if _, _, err := tx.Set("hello", "planet", nil); err != ErrTxNotWritable {
			t.Fatal("expecting a tx not writable error")
		}
		if _, err := tx.Delete("hello"); err != ErrTxNotWritable {
			t.Fatal("expecting a tx not writable error")
		}
		tx.writable = true
		if _, err := tx.Get("something"); err != ErrNotFound {
			t.Fatalf("expecting not found error")
		}
		if _, err := tx.Delete("something"); err != ErrNotFound {
			t.Fatalf("expecting not found error")
		}
		if _, _, err := tx.Set("var", "val", &SetOptions{Expires: true, TTL: 0}); err != nil {
			t.Fatal(err)
		}
		if _, err := tx.Get("var"); err != ErrNotFound {
			t.Fatalf("expecting not found error")
		}
		if _, err := tx.Delete("var"); err != ErrNotFound {
			tx.unlock()
			t.Fatalf("expecting not found error")
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}

	// test for invalid commits
	if err := db.Update(func(tx *Tx) error {
		// we are going to do some hackery
		defer func() {
			if v := recover(); v != nil {
				if v.(string) != "managed tx commit not allowed" {
					t.Fatal(v.(string))
				}
			}
		}()
		return tx.commit()
	}); err != nil {
		t.Fatal(err)
	}

	// test for invalid commits
	if err := db.Update(func(tx *Tx) error {
		// we are going to do some hackery
		defer func() {
			if v := recover(); v != nil {
				if v.(string) != "managed tx rollback not allowed" {
					t.Fatal(v.(string))
				}
			}
		}()
		return tx.rollback()
	}); err != nil {
		t.Fatal(err)
	}

	// test for closed transactions
	if err := db.Update(func(tx *Tx) error {
		tx.db = nil
		return nil
	}); err != ErrTxClosed {
		t.Fatal("expecting tx closed error")
	}
	db.mu.Unlock()

	// test for invalid writes
	if err := db.Update(func(tx *Tx) error {
		tx.writable = false
		return nil
	}); err != ErrTxNotWritable {
		t.Fatal("expecting tx not writable error")
	}
	db.mu.Unlock()
	// test for closed transactions
	if err := db.View(func(tx *Tx) error {
		tx.db = nil
		return nil
	}); err != ErrTxClosed {
		t.Fatal("expecting tx closed error")
	}
	db.mu.RUnlock()
	// flush to unwritable file
	if err := db.Update(func(tx *Tx) error {
		_, _, err := tx.Set("var1", "val1", nil)
		if err != nil {
			t.Fatal(err)
		}
		return tx.db.file.Close()
	}); err == nil {
		t.Fatal("should not be able to commit when the file is closed")
	}
	db.file, err = os.OpenFile("data.db", os.O_CREATE|os.O_RDWR, 0666)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.file.Seek(0, 2); err != nil {
		t.Fatal(err)
	}
	db.bufw = bufio.NewWriter(db.file)
	if err := db.CreateIndex("blank", "*", nil); err != nil {
		t.Fatal(err)
	}
	// test scanning
	if err := db.Update(func(tx *Tx) error {
		_, _, err := tx.Set("nothing", "here", nil)
		return err
	}); err != nil {
		t.Fatal(err)
	}
	if err := db.View(func(tx *Tx) error {
		s := ""
		err := tx.Ascend("", func(key, val string) bool {
			s += key + ":" + val + "\n"
			return true
		})
		if err != nil {
			return err
		}
		if s != "hello:planet\nnothing:here\n" {
			t.Fatal("invalid scan")
		}
		tx.db = nil
		err = tx.Ascend("", func(key, val string) bool { return true })
		if err != ErrTxClosed {
			tx.unlock()
			t.Fatal("expecting tx closed error")
		}
		tx.db = db
		err = tx.Ascend("na", func(key, val string) bool { return true })
		if err != ErrNotFound {
			t.Fatal("expecting not found error")
		}
		err = tx.Ascend("blank", func(key, val string) bool { return true })
		if err != nil {
			t.Fatal(err)
		}
		s = ""
		err = tx.AscendLessThan("", "liger", func(key, val string) bool {
			s += key + ":" + val + "\n"
			return true
		})
		if err != nil {
			return err
		}
		if s != "hello:planet\n" {
			t.Fatal("invalid scan")
		}

		s = ""
		err = tx.Descend("", func(key, val string) bool {
			s += key + ":" + val + "\n"
			return true
		})
		if err != nil {
			return err
		}
		if s != "nothing:here\nhello:planet\n" {
			t.Fatal("invalid scan")
		}

		s = ""
		err = tx.DescendLessOrEqual("", "liger", func(key, val string) bool {
			s += key + ":" + val + "\n"
			return true
		})
		if err != nil {
			return err
		}

		if s != "hello:planet\n" {
			t.Fatal("invalid scan")
		}

		s = ""
		err = tx.DescendGreaterThan("", "liger", func(key, val string) bool {
			s += key + ":" + val + "\n"
			return true
		})
		if err != nil {
			return err
		}

		if s != "nothing:here\n" {
			t.Fatal("invalid scan")
		}
		s = ""
		err = tx.DescendRange("", "liger", "apple", func(key, val string) bool {
			s += key + ":" + val + "\n"
			return true
		})
		if err != nil {
			return err
		}
		if s != "hello:planet\n" {
			t.Fatal("invalid scan")
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}

	// test some spatial stuff
	if err := db.CreateSpatialIndex("spat", "rect:*", IndexRect); err != nil {
		t.Fatal(err)
	}
	if err := db.CreateSpatialIndex("junk", "rect:*", nil); err != nil {
		t.Fatal(err)
	}
	err = db.Update(func(tx *Tx) error {
		if _, _, err := tx.Set("rect:1", "[10 10],[20 20]", nil); err != nil {
			return err
		}
		if _, _, err := tx.Set("rect:2", "[15 15],[25 25]", nil); err != nil {
			return err
		}
		if _, _, err := tx.Set("shape:1", "[12 12],[25 25]", nil); err != nil {
			return err
		}
		s := ""
		err := tx.Intersects("spat", "[5 5],[13 13]", func(key, val string) bool {
			s += key + ":" + val + "\n"
			return true
		})
		if err != nil {
			return err
		}
		if s != "rect:1:[10 10],[20 20]\n" {
			t.Fatal("invalid scan")
		}
		tx.db = nil
		err = tx.Intersects("spat", "[5 5],[13 13]", func(key, val string) bool {
			return true
		})
		if err != ErrTxClosed {
			t.Fatal("expecting tx closed error")
		}
		tx.db = db
		err = tx.Intersects("", "[5 5],[13 13]", func(key, val string) bool {
			return true
		})
		if err != nil {
			t.Fatal(err)
		}
		err = tx.Intersects("na", "[5 5],[13 13]", func(key, val string) bool {
			return true
		})
		if err != ErrNotFound {
			t.Fatal("expecting not found error")
		}
		err = tx.Intersects("junk", "[5 5],[13 13]", func(key, val string) bool {
			return true
		})
		if err != nil {
			t.Fatal(err)
		}
		n, err := tx.Len()
		if err != nil {
			t.Fatal(err)
		}
		if n != 5 {
			t.Fatalf("expecting %v, got %v", 5, n)
		}
		tx.db = nil
		_, err = tx.Len()
		if err != ErrTxClosed {
			t.Fatal("expecting tx closed error")
		}
		tx.db = db
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	// test after closing
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}

	if err := db.Update(func(tx *Tx) error { return nil }); err != ErrDatabaseClosed {
		t.Fatalf("should not be able to perform transactionso on a closed database.")
	}
}
func TestNoExpiringItem(t *testing.T) {
	item := &dbItem{key: "key", val: "val"}
	if !item.expiresAt().Equal(maxTime) {
		t.Fatal("item.expiresAt() != maxTime")
	}
	if min, max := item.Rect(nil); min != nil || max != nil {
		t.Fatal("item min,max should both be nil")
	}
}
func TestAutoShrink(t *testing.T) {
	if err := os.RemoveAll("data.db"); err != nil {
		t.Fatal(err)
	}
	db, err := Open("data.db")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = os.RemoveAll("data.db") }()
	defer func() { _ = db.Close() }()
	for i := 0; i < 1000; i++ {
		err = db.Update(func(tx *Tx) error {
			for i := 0; i < 20; i++ {
				if _, _, err := tx.Set(fmt.Sprintf("HELLO:%d", i), "WORLD", nil); err != nil {
					return err
				}
			}
			return nil
		})
		if err != nil {
			t.Fatal(err)
		}
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	db, err = Open("data.db")
	if err != nil {
		t.Fatal(err)
	}
	db.config.AutoShrinkMinSize = 64 * 1024 // 64K
	for i := 0; i < 2000; i++ {
		err = db.Update(func(tx *Tx) error {
			for i := 0; i < 20; i++ {
				if _, _, err := tx.Set(fmt.Sprintf("HELLO:%d", i), "WORLD", nil); err != nil {
					return err
				}
			}
			return nil
		})
		if err != nil {
			t.Fatal(err)
		}
	}
	time.Sleep(time.Second * 3)
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	db, err = Open("data.db")
	if err != nil {
		t.Fatal(err)
	}
	err = db.View(func(tx *Tx) error {
		n, err := tx.Len()
		if err != nil {
			return err
		}
		if n != 20 {
			t.Fatalf("expecting 20, got %v", n)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}

// test database format loading
func TestDatabaseFormat(t *testing.T) {
	// should succeed
	func() {
		resp := strings.Join([]string{
			"*3\r\n$3\r\nset\r\n$4\r\nvar1\r\n$4\r\n1234\r\n",
			"*3\r\n$3\r\nset\r\n$4\r\nvar2\r\n$4\r\n1234\r\n",
			"*2\r\n$3\r\ndel\r\n$4\r\nvar1\r\n",
			"*5\r\n$3\r\nset\r\n$3\r\nvar\r\n$3\r\nval\r\n$2\r\nex\r\n$2\r\n10\r\n",
		}, "")
		if err := os.RemoveAll("data.db"); err != nil {
			t.Fatal(err)
		}
		if err := ioutil.WriteFile("data.db", []byte(resp), 0666); err != nil {
			t.Fatal(err)
		}
		db, err := Open("data.db")
		if err != nil {
			t.Fatal(err)
		}
		defer func() { _ = os.RemoveAll("data.db") }()
		defer func() { _ = db.Close() }()
	}()
	testBadFormat := func(resp string) {
		if err := os.RemoveAll("data.db"); err != nil {
			t.Fatal(err)
		}
		if err := ioutil.WriteFile("data.db", []byte(resp), 0666); err != nil {
			t.Fatal(err)
		}
		db, err := Open("data.db")
		if err == nil {
			if err := db.Close(); err != nil {
				t.Fatal(err)
			}
			if err := os.RemoveAll("data.db"); err != nil {
				t.Fatal(err)
			}
			t.Fatalf("invalid database should not be allowed")
		}
	}
	testBadFormat("*3\r")
	testBadFormat("*3\n")
	testBadFormat("*a\r\n")
	testBadFormat("*2\r\n")
	testBadFormat("*2\r\n%3")
	testBadFormat("*2\r\n$")
	testBadFormat("*2\r\n$3\r\n")
	testBadFormat("*2\r\n$3\r\ndel")
	testBadFormat("*2\r\n$3\r\ndel\r\r")
	testBadFormat("*0\r\n*2\r\n$3\r\ndel\r\r")
	testBadFormat("*1\r\n$3\r\nnop\r\n")
	testBadFormat("*1\r\n$3\r\ndel\r\n")
	testBadFormat("*1\r\n$3\r\nset\r\n")
	testBadFormat("*5\r\n$3\r\nset\r\n$3\r\nvar\r\n$3\r\nval\r\n$2\r\nxx\r\n$2\r\n10\r\n")
	testBadFormat("*5\r\n$3\r\nset\r\n$3\r\nvar\r\n$3\r\nval\r\n$2\r\nex\r\n$2\r\naa\r\n")
}

func TestInsertsAndDeleted(t *testing.T) {
	if err := os.RemoveAll("data.db"); err != nil {
		t.Fatal(err)
	}
	db, err := Open("data.db")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = os.RemoveAll("data.db") }()
	defer func() { _ = db.Close() }()
	if err := db.CreateIndex("any", "*", IndexString); err != nil {
		t.Fatal(err)
	}
	if err := db.CreateSpatialIndex("rect", "*", IndexRect); err != nil {
		t.Fatal(err)
	}
	if err := db.Update(func(tx *Tx) error {
		if _, _, err := tx.Set("item1", "value1", &SetOptions{Expires: true, TTL: time.Second}); err != nil {
			return err
		}
		if _, _, err := tx.Set("item2", "value2", nil); err != nil {
			return err
		}
		if _, _, err := tx.Set("item3", "value3", &SetOptions{Expires: true, TTL: time.Second}); err != nil {
			return err
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}

	// test replacing items in the database
	if err := db.Update(func(tx *Tx) error {
		if _, _, err := tx.Set("item1", "nvalue1", nil); err != nil {
			return err
		}
		if _, _, err := tx.Set("item2", "nvalue2", nil); err != nil {
			return err
		}
		if _, err := tx.Delete("item3"); err != nil {
			return err
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
}

// test index compare functions
func TestIndexCompare(t *testing.T) {
	if !IndexFloat("1.5", "1.6") {
		t.Fatalf("expected true, got false")
	}
	if !IndexInt("-1", "2") {
		t.Fatalf("expected true, got false")
	}
	if !IndexUint("10", "25") {
		t.Fatalf("expected true, got false")
	}
	if !IndexBinary("Hello", "hello") {
		t.Fatalf("expected true, got false")
	}
	if IndexString("hello", "hello") {
		t.Fatalf("expected false, got true")
	}
	if IndexString("Hello", "hello") {
		t.Fatalf("expected false, got true")
	}
	if IndexString("hello", "Hello") {
		t.Fatalf("expected false, got true")
	}
	if !IndexString("gello", "Hello") {
		t.Fatalf("expected true, got false")
	}
	if IndexString("Hello", "gello") {
		t.Fatalf("expected false, got true")
	}
	if Rect(IndexRect("[1 2 3 4],[5 6 7 8]")) != "[1 2 3 4],[5 6 7 8]" {
		t.Fatalf("expected '%v', got '%v'", "[1 2 3 4],[5 6 7 8]", Rect(IndexRect("[1 2 3 4],[5 6 7 8]")))
	}
	if Rect(IndexRect("[1 2 3 4]")) != "[1 2 3 4]" {
		t.Fatalf("expected '%v', got '%v'", "[1 2 3 4]", Rect(IndexRect("[1 2 3 4]")))
	}
	if Rect(nil, nil) != "" {
		t.Fatalf("expected '%v', got '%v'", "", Rect(nil, nil))
	}
	if Point(1, 2, 3) != "[1 2 3]" {
		t.Fatalf("expected '%v', got '%v'", "[1 2 3]", Point(1, 2, 3))
	}
}

// test opening a folder.
func TestOpeningAFolder(t *testing.T) {
	if err := os.RemoveAll("dir.tmp"); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir("dir.tmp", 0700); err != nil {
		t.Fatal(err)
	}
	defer func() { _ = os.RemoveAll("dir.tmp") }()
	db, err := Open("dir.tmp")
	if err == nil {
		if err := db.Close(); err != nil {
			t.Fatal(err)
		}
		t.Fatalf("opening a directory should not be allowed")
	}
}

// test opening an invalid resp file.
func TestOpeningInvalidDatabaseFile(t *testing.T) {
	if err := os.RemoveAll("data.db"); err != nil {
		t.Fatal(err)
	}
	if err := ioutil.WriteFile("data.db", []byte("invalid\r\nfile"), 0666); err != nil {
		t.Fatal(err)
	}
	defer func() { _ = os.RemoveAll("data.db") }()
	db, err := Open("data.db")
	if err == nil {
		if err := db.Close(); err != nil {
			t.Fatal(err)
		}
		t.Fatalf("invalid database should not be allowed")
	}
}

// test closing a closed database.
func TestOpeningClosedDatabase(t *testing.T) {
	if err := os.RemoveAll("data.db"); err != nil {
		t.Fatal(err)
	}
	db, err := Open("data.db")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = os.RemoveAll("data.db") }()
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	if err := db.Close(); err != ErrDatabaseClosed {
		t.Fatal("should not be able to close a closed database")
	}
	db, err = Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	if err := db.Close(); err != ErrDatabaseClosed {
		t.Fatal("should not be able to close a closed database")
	}
}

// test shrinking a database.
func TestShrink(t *testing.T) {
	if err := os.RemoveAll("data.db"); err != nil {
		t.Fatal(err)
	}
	db, err := Open("data.db")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = os.RemoveAll("data.db") }()
	defer func() { _ = db.Close() }()
	if err := db.Shrink(); err != nil {
		t.Fatal(err)
	}
	fi, err := os.Stat("data.db")
	if err != nil {
		t.Fatal(err)
	}
	if fi.Size() != 0 {
		t.Fatalf("expected %v, got %v", 0, fi.Size())
	}
	// add 10 items
	err = db.Update(func(tx *Tx) error {
		for i := 0; i < 10; i++ {
			if _, _, err := tx.Set(fmt.Sprintf("key%d", i), fmt.Sprintf("val%d", i), nil); err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	// add the same 10 items
	// this will create 10 duplicate log entries
	err = db.Update(func(tx *Tx) error {
		for i := 0; i < 10; i++ {
			if _, _, err := tx.Set(fmt.Sprintf("key%d", i), fmt.Sprintf("val%d", i), nil); err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	fi, err = os.Stat("data.db")
	if err != nil {
		t.Fatal(err)
	}
	sz1 := fi.Size()
	if sz1 == 0 {
		t.Fatalf("expected > 0, got %v", sz1)
	}
	if err := db.Shrink(); err != nil {
		t.Fatal(err)
	}
	fi, err = os.Stat("data.db")
	if err != nil {
		t.Fatal(err)
	}
	sz2 := fi.Size()
	if sz2 >= sz1 {
		t.Fatalf("expected < %v, got %v", sz1, sz2)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	if err := db.Shrink(); err != ErrDatabaseClosed {
		t.Fatal("shrink on a closed databse should not be allowed")
	}
	// Now we will open a db that does not persist
	db, err = Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = db.Close() }()
	// add 10 items
	err = db.Update(func(tx *Tx) error {
		for i := 0; i < 10; i++ {
			if _, _, err := tx.Set(fmt.Sprintf("key%d", i), fmt.Sprintf("val%d", i), nil); err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	// add the same 10 items
	// this will create 10 duplicate log entries
	err = db.Update(func(tx *Tx) error {
		for i := 0; i < 10; i++ {
			if _, _, err := tx.Set(fmt.Sprintf("key%d", i), fmt.Sprintf("val%d", i), nil); err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	err = db.View(func(tx *Tx) error {
		n, err := tx.Len()
		if err != nil {
			t.Fatal(err)
		}
		if n != 10 {
			t.Fatalf("expecting %v, got %v", 10, n)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	// this should succeed even though it's basically a noop.
	if err := db.Shrink(); err != nil {
		t.Fatal(err)
	}
}

func TestVariousIndexOperations(t *testing.T) {
	if err := os.RemoveAll("data.db"); err != nil {
		t.Fatal(err)
	}
	db, err := Open("data.db")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = os.RemoveAll("data.db") }()
	defer func() { _ = db.Close() }()
	// test creating an index with no index name.
	err = db.CreateIndex("", "", nil)
	if err == nil {
		t.Fatal("should not be able to create an index with no name")
	}
	// test creating an index with a name that has already been used.
	err = db.CreateIndex("hello", "", nil)
	if err != nil {
		t.Fatal(err)
	}
	err = db.CreateIndex("hello", "", nil)
	if err == nil {
		t.Fatal("should not be able to create a duplicate index")
	}
	err = db.Update(func(tx *Tx) error {

		if _, _, err := tx.Set("user:1", "tom", nil); err != nil {
			return err
		}
		if _, _, err := tx.Set("user:2", "janet", nil); err != nil {
			return err
		}
		if _, _, err := tx.Set("alt:1", "from", nil); err != nil {
			return err
		}
		if _, _, err := tx.Set("alt:2", "there", nil); err != nil {
			return err
		}
		if _, _, err := tx.Set("rect:1", "[1 2],[3 4]", nil); err != nil {
			return err
		}
		if _, _, err := tx.Set("rect:2", "[5 6],[7 8]", nil); err != nil {
			return err
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	// test creating an index after adding items. use pattern matching. have some items in the match and some not.
	if err := db.CreateIndex("string", "user:*", IndexString); err != nil {
		t.Fatal(err)
	}
	// test creating a spatial index after adding items. use pattern matching. have some items in the match and some not.
	if err := db.CreateSpatialIndex("rect", "rect:*", IndexRect); err != nil {
		t.Fatal(err)
	}
	// test dropping an index
	if err := db.DropIndex("hello"); err != nil {
		t.Fatal(err)
	}
	// test dropping an index with no name
	if err := db.DropIndex(""); err == nil {
		t.Fatal("should not be allowed to drop an index with no name")
	}
	// test dropping an index with no name
	if err := db.DropIndex("na"); err == nil {
		t.Fatal("should not be allowed to drop an index that does not exist")
	}
	// test retrieving index names
	names, err := db.Indexes()
	if err != nil {
		t.Fatal(err)
	}
	if strings.Join(names, ",") != "rect,string" {
		t.Fatalf("expecting '%v', got '%v'", "rect,string", strings.Join(names, ","))
	}
	// test creating an index after closing database
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	if err := db.CreateIndex("new-index", "", nil); err != ErrDatabaseClosed {
		t.Fatal("should not be able to create an index on a closed database")
	}
	// test getting index names after closing database
	if _, err := db.Indexes(); err != ErrDatabaseClosed {
		t.Fatal("should not be able to get index names on a closed database")
	}
	// test dropping an index after closing database
	if err := db.DropIndex("rect"); err != ErrDatabaseClosed {
		t.Fatal("should not be able to drop an index on a closed database")
	}
}

func test(t *testing.T, a, b bool) {
	if a != b {
		t.Fatal("failed, bummer...")
	}
}

func TestPatternMatching(t *testing.T) {
	test(t, wildcardMatch("hello", "hello"), true)
	test(t, wildcardMatch("hello", "h*"), true)
	test(t, wildcardMatch("hello", "h*o"), true)
	test(t, wildcardMatch("hello", "h*l*o"), true)
	test(t, wildcardMatch("hello", "h*z*o"), false)
	test(t, wildcardMatch("hello", "*l*o"), true)
	test(t, wildcardMatch("hello", "*l*"), true)
	test(t, wildcardMatch("hello", "*?*"), true)
	test(t, wildcardMatch("hello", "*"), true)
	test(t, wildcardMatch("hello", "h?llo"), true)
	test(t, wildcardMatch("hello", "h?l?o"), true)
	test(t, wildcardMatch("", "*"), true)
	test(t, wildcardMatch("", ""), true)
	test(t, wildcardMatch("h", ""), false)
	test(t, wildcardMatch("", "?"), false)
}

func TestBasic(t *testing.T) {
	rand.Seed(time.Now().UnixNano())
	if err := os.RemoveAll("data.db"); err != nil {
		t.Fatal(err)
	}
	db, err := Open("data.db")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = os.RemoveAll("data.db") }()
	defer func() { _ = db.Close() }()

	// create a simple index
	if err := db.CreateIndex("users", "fun:user:*", IndexString); err != nil {
		t.Fatal(err)
	}

	// create a spatial index
	if err := db.CreateSpatialIndex("rects", "rect:*", IndexRect); err != nil {
		t.Fatal(err)
	}
	if true {
		err := db.Update(func(tx *Tx) error {
			if _, _, err := tx.Set("fun:user:0", "tom", nil); err != nil {
				return err
			}
			if _, _, err := tx.Set("fun:user:1", "Randi", nil); err != nil {
				return err
			}
			if _, _, err := tx.Set("fun:user:2", "jane", nil); err != nil {
				return err
			}
			if _, _, err := tx.Set("fun:user:4", "Janet", nil); err != nil {
				return err
			}
			if _, _, err := tx.Set("fun:user:5", "Paula", nil); err != nil {
				return err
			}
			if _, _, err := tx.Set("fun:user:6", "peter", nil); err != nil {
				return err
			}
			if _, _, err := tx.Set("fun:user:7", "Terri", nil); err != nil {
				return err
			}
			return nil
		})
		if err != nil {
			t.Fatal(err)
		}
		// add some random items
		start := time.Now()
		if err := db.Update(func(tx *Tx) error {
			for _, i := range rand.Perm(100) {
				if _, _, err := tx.Set(fmt.Sprintf("tag:%d", i+100), fmt.Sprintf("val:%d", rand.Int()%100+100), nil); err != nil {
					return err
				}
			}
			return nil
		}); err != nil {
			t.Fatal(err)
		}
		if false {
			println(time.Now().Sub(start).String(), db.keys.Len())
		}
		// add some random rects
		if err := db.Update(func(tx *Tx) error {
			if _, _, err := tx.Set("rect:1", Rect([]float64{10, 10}, []float64{20, 20}), nil); err != nil {
				return err
			}
			if _, _, err := tx.Set("rect:2", Rect([]float64{15, 15}, []float64{24, 24}), nil); err != nil {
				return err
			}
			if _, _, err := tx.Set("rect:3", Rect([]float64{17, 17}, []float64{27, 27}), nil); err != nil {
				return err
			}
			return nil
		}); err != nil {
			t.Fatal(err)
		}
	}
	// verify the data has been created
	buf := &bytes.Buffer{}
	err = db.View(func(tx *Tx) error {
		err = tx.Ascend("users", func(key, val string) bool {
			fmt.Fprintf(buf, "%s %s\n", key, val)
			return true
		})
		if err != nil {
			t.Fatal(err)
		}
		err = tx.AscendRange("", "tag:170", "tag:172", func(key, val string) bool {
			fmt.Fprintf(buf, "%s\n", key)
			return true
		})
		if err != nil {
			t.Fatal(err)
		}
		err = tx.AscendGreaterOrEqual("", "tag:195", func(key, val string) bool {
			fmt.Fprintf(buf, "%s\n", key)
			return true
		})
		if err != nil {
			t.Fatal(err)
		}
		err = tx.AscendGreaterOrEqual("", "rect:", func(key, val string) bool {
			if !strings.HasPrefix(key, "rect:") {
				return false
			}
			min, max := IndexRect(val)
			fmt.Fprintf(buf, "%s: %v,%v\n", key, min, max)
			return true
		})
		expect := make([]string, 2)
		n := 0
		err = tx.Intersects("rects", "[0 0],[15 15]", func(key, val string) bool {
			if n == 2 {
				t.Fatalf("too many rects where received, expecting only two")
			}
			min, max := IndexRect(val)
			s := fmt.Sprintf("%s: %v,%v\n", key, min, max)
			if key == "rect:1" {
				expect[0] = s
			} else if key == "rect:2" {
				expect[1] = s
			}
			n++
			return true
		})
		if err != nil {
			t.Fatal(err)
		}
		for _, s := range expect {
			if _, err := buf.WriteString(s); err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	res := `
fun:user:2 jane
fun:user:4 Janet
fun:user:5 Paula
fun:user:6 peter
fun:user:1 Randi
fun:user:7 Terri
fun:user:0 tom
tag:170
tag:171
tag:195
tag:196
tag:197
tag:198
tag:199
rect:1: [10 10],[20 20]
rect:2: [15 15],[24 24]
rect:3: [17 17],[27 27]
rect:1: [10 10],[20 20]
rect:2: [15 15],[24 24]
`
	res = strings.Replace(res, "\r", "", -1)
	if strings.TrimSpace(buf.String()) != strings.TrimSpace(res) {
		t.Fatalf("expected [%v], got [%v]", strings.TrimSpace(res), strings.TrimSpace(buf.String()))
	}
}
func testRectStringer(min, max []float64) error {
	nmin, nmax := IndexRect(Rect(min, max))
	if len(nmin) != len(min) {
		return fmt.Errorf("rect=%v,%v, expect=%v,%v", nmin, nmax, min, max)
	}
	for i := 0; i < len(min); i++ {
		if min[i] != nmin[i] || max[i] != nmax[i] {
			return fmt.Errorf("rect=%v,%v, expect=%v,%v", nmin, nmax, min, max)
		}
	}
	return nil
}
func TestRectStrings(t *testing.T) {
	test(t, Rect(IndexRect(Point(1))) == "[1]", true)
	test(t, Rect(IndexRect(Point(1, 2, 3, 4))) == "[1 2 3 4]", true)
	test(t, Rect(IndexRect(Rect(IndexRect("[1 2],[1 2]")))) == "[1 2]", true)
	test(t, Rect(IndexRect(Rect(IndexRect("[1 2],[2 2]")))) == "[1 2],[2 2]", true)
	test(t, Rect(IndexRect(Rect(IndexRect("[1 2],[2 2],[3]")))) == "[1 2],[2 2]", true)
	test(t, Rect(IndexRect(Rect(IndexRect("[1 2]")))) == "[1 2]", true)
	test(t, Rect(IndexRect(Rect(IndexRect("[1.5 2 4.5 5.6]")))) == "[1.5 2 4.5 5.6]", true)
	test(t, Rect(IndexRect(Rect(IndexRect("[1.5 2 4.5 5.6 -1],[]")))) == "[1.5 2 4.5 5.6 -1],[]", true)
	test(t, Rect(IndexRect(Rect(IndexRect("[]")))) == "[]", true)
	test(t, Rect(IndexRect(Rect(IndexRect("")))) == "", true)
	if err := testRectStringer(nil, nil); err != nil {
		t.Fatal(err)
	}
	if err := testRectStringer([]float64{}, []float64{}); err != nil {
		t.Fatal(err)
	}
	if err := testRectStringer([]float64{1}, []float64{2}); err != nil {
		t.Fatal(err)
	}
	if err := testRectStringer([]float64{1, 2}, []float64{3, 4}); err != nil {
		t.Fatal(err)
	}
	if err := testRectStringer([]float64{1, 2, 3}, []float64{4, 5, 6}); err != nil {
		t.Fatal(err)
	}
	if err := testRectStringer([]float64{1, 2, 3, 4}, []float64{5, 6, 7, 8}); err != nil {
		t.Fatal(err)
	}
	if err := testRectStringer([]float64{1, 2, 3, 4, 5}, []float64{6, 7, 8, 9, 0}); err != nil {
		t.Fatal(err)
	}
}

func TestTTL(t *testing.T) {
	if err := os.RemoveAll("data.db"); err != nil {
		t.Fatal(err)
	}
	db, err := Open("data.db")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = os.RemoveAll("data.db") }()
	defer func() { _ = db.Close() }()
	err = db.Update(func(tx *Tx) error {
		if _, _, err := tx.Set("key1", "val1", &SetOptions{Expires: true, TTL: time.Second}); err != nil {
			return err
		}
		if _, _, err := tx.Set("key2", "val2", nil); err != nil {
			return err
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	err = db.View(func(tx *Tx) error {
		dur1, err := tx.TTL("key1")
		if err != nil {
			t.Fatal(err)
		}
		if dur1 > time.Second || dur1 <= 0 {
			t.Fatalf("expecting between zero and one second, got '%v'", dur1)
		}
		dur1, err = tx.TTL("key2")
		if err != nil {
			t.Fatal(err)
		}
		if dur1 >= 0 {
			t.Fatalf("expecting a negative value, got '%v'", dur1)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}

func TestConfig(t *testing.T) {
	if err := os.RemoveAll("data.db"); err != nil {
		t.Fatal(err)
	}
	db, err := Open("data.db")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = os.RemoveAll("data.db") }()
	defer func() { _ = db.Close() }()

	err = db.SetConfig(Config{SyncPolicy: SyncPolicy(-1)})
	if err == nil {
		t.Fatal("expecting a config syncpolicy error")
	}
	err = db.SetConfig(Config{SyncPolicy: SyncPolicy(3)})
	if err == nil {
		t.Fatal("expecting a config syncpolicy error")
	}
	err = db.SetConfig(Config{SyncPolicy: Never})
	if err != nil {
		t.Fatal(err)
	}
	err = db.SetConfig(Config{SyncPolicy: EverySecond})
	if err != nil {
		t.Fatal(err)
	}
	err = db.SetConfig(Config{AutoShrinkMinSize: 100, AutoShrinkPercentage: 200, SyncPolicy: Always})
	if err != nil {
		t.Fatal(err)
	}

	var c Config
	if err := db.ReadConfig(&c); err != nil {
		t.Fatal(err)
	}
	if c.AutoShrinkMinSize != 100 || c.AutoShrinkPercentage != 200 && c.SyncPolicy != Always {
		t.Fatalf("expecting %v, %v, and %v, got %v, %v, and %v", 100, 200, Always, c.AutoShrinkMinSize, c.AutoShrinkPercentage, c.SyncPolicy)
	}
}
func testUint64Hex(n uint64) string {
	s := strconv.FormatUint(n, 16)
	s = "0000000000000000" + s
	return s[len(s)-16:]
}
func textHexUint64(s string) uint64 {
	n, _ := strconv.ParseUint(s, 16, 64)
	return n
}
func benchClose(t *testing.B, persist bool, db *DB) {
	if persist {
		if err := os.RemoveAll("data.db"); err != nil {
			t.Fatal(err)
		}
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
}

func benchOpenFillData(t *testing.B, N int,
	set, persist, random bool,
	geo bool,
	batch int) (db *DB, keys, vals []string) {
	///
	t.StopTimer()
	rand.Seed(time.Now().UnixNano())
	var err error
	if persist {
		if err := os.RemoveAll("data.db"); err != nil {
			t.Fatal(err)
		}
		db, err = Open("data.db")
	} else {
		db, err = Open(":memory:")
	}
	if err != nil {
		t.Fatal(err)
	}
	keys = make([]string, N)
	vals = make([]string, N)
	perm := rand.Perm(N)
	for i := 0; i < N; i++ {
		if random && set {
			keys[perm[i]] = testUint64Hex(uint64(i))
			vals[perm[i]] = strconv.FormatInt(rand.Int63()%1000+1000, 10)
		} else {
			keys[i] = testUint64Hex(uint64(i))
			vals[i] = strconv.FormatInt(rand.Int63()%1000+1000, 10)
		}
	}
	if set {
		t.StartTimer()
	}
	for i := 0; i < N; {
		err := db.Update(func(tx *Tx) error {
			var err error
			for j := 0; j < batch && i < N; j++ {
				_, _, err = tx.Set(keys[i], vals[i], nil)
				i++
			}
			return err
		})
		if err != nil {
			t.Fatal(err)
		}
	}
	if set {
		t.StopTimer()
	}
	var n uint64
	err = db.View(func(tx *Tx) error {
		err := tx.Ascend("", func(key, value string) bool {
			n2 := textHexUint64(key)
			if n2 != n {
				t.Fatalf("expecting '%v', got '%v'", n2, n)
			}
			n++
			return true
		})
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
	if n != uint64(N) {
		t.Fatalf("expecting '%v', got '%v'", N, n)
	}
	t.StartTimer()
	return db, keys, vals
}

func benchSetGet(t *testing.B, set, persist, random bool, batch int) {
	N := t.N
	for N > 0 {
		n := 0
		if N >= 100000 {
			n = 100000
		} else {
			n = N
		}
		func() {
			db, keys, _ := benchOpenFillData(t, n, set, persist, random, false, batch)
			defer benchClose(t, persist, db)
			if !set {
				for i := 0; i < n; {
					err := db.View(func(tx *Tx) error {
						var err error
						for j := 0; j < batch && i < n; j++ {
							_, err = tx.Get(keys[i])
							i++
						}
						return err
					})
					if err != nil {
						t.Fatal(err)
					}
				}
			}
		}()
		N -= n
	}
}

// Set Persist
func Benchmark_Set_Persist_Random_1(t *testing.B) {
	benchSetGet(t, true, true, true, 1)
}
func Benchmark_Set_Persist_Random_10(t *testing.B) {
	benchSetGet(t, true, true, true, 10)
}
func Benchmark_Set_Persist_Random_100(t *testing.B) {
	benchSetGet(t, true, true, true, 100)
}
func Benchmark_Set_Persist_Sequential_1(t *testing.B) {
	benchSetGet(t, true, true, false, 1)
}
func Benchmark_Set_Persist_Sequential_10(t *testing.B) {
	benchSetGet(t, true, true, false, 10)
}
func Benchmark_Set_Persist_Sequential_100(t *testing.B) {
	benchSetGet(t, true, true, false, 100)
}

// Set NoPersist
func Benchmark_Set_NoPersist_Random_1(t *testing.B) {
	benchSetGet(t, true, false, true, 1)
}
func Benchmark_Set_NoPersist_Random_10(t *testing.B) {
	benchSetGet(t, true, false, true, 10)
}
func Benchmark_Set_NoPersist_Random_100(t *testing.B) {
	benchSetGet(t, true, false, true, 100)
}
func Benchmark_Set_NoPersist_Sequential_1(t *testing.B) {
	benchSetGet(t, true, false, false, 1)
}
func Benchmark_Set_NoPersist_Sequential_10(t *testing.B) {
	benchSetGet(t, true, false, false, 10)
}
func Benchmark_Set_NoPersist_Sequential_100(t *testing.B) {
	benchSetGet(t, true, false, false, 100)
}

// Get
func Benchmark_Get_1(t *testing.B) {
	benchSetGet(t, false, false, false, 1)
}
func Benchmark_Get_10(t *testing.B) {
	benchSetGet(t, false, false, false, 10)
}
func Benchmark_Get_100(t *testing.B) {
	benchSetGet(t, false, false, false, 100)
}

func benchScan(t *testing.B, asc bool, count int) {
	N := count
	db, _, _ := benchOpenFillData(t, N, false, false, false, false, 100)
	defer benchClose(t, false, db)
	for i := 0; i < t.N; i++ {
		count := 0
		err := db.View(func(tx *Tx) error {
			if asc {
				return tx.Ascend("", func(key, val string) bool {
					count++
					return true
				})
			}
			return tx.Descend("", func(key, val string) bool {
				count++
				return true
			})

		})
		if err != nil {
			t.Fatal(err)
		}
		if count != N {
			t.Fatalf("expecting '%v', got '%v'", N, count)
		}
	}
}

func Benchmark_Ascend_1(t *testing.B) {
	benchScan(t, true, 1)
}
func Benchmark_Ascend_10(t *testing.B) {
	benchScan(t, true, 10)
}
func Benchmark_Ascend_100(t *testing.B) {
	benchScan(t, true, 100)
}
func Benchmark_Ascend_1000(t *testing.B) {
	benchScan(t, true, 1000)
}
func Benchmark_Ascend_10000(t *testing.B) {
	benchScan(t, true, 10000)
}

func Benchmark_Descend_1(t *testing.B) {
	benchScan(t, false, 1)
}
func Benchmark_Descend_10(t *testing.B) {
	benchScan(t, false, 10)
}
func Benchmark_Descend_100(t *testing.B) {
	benchScan(t, false, 100)
}
func Benchmark_Descend_1000(t *testing.B) {
	benchScan(t, false, 1000)
}
func Benchmark_Descend_10000(t *testing.B) {
	benchScan(t, false, 10000)
}

/*
func Benchmark_Spatial_2D(t *testing.B) {
	N := 100000
	db, _, _ := benchOpenFillData(t, N, true, true, false, true, 100)
	defer benchClose(t, false, db)

}
*/
