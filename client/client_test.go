// Copyright 2016 Keybase Inc. All rights reserved.
// Use of this source code is governed by a BSD
// license that can be found in the LICENSE file.

package client

import (
	"encoding/json"
	"io/ioutil"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strconv"
	"testing"

	"github.com/keybase/kbfs/libkbfs"
	sserver1 "github.com/keybase/search/protocol/sserver"
	"golang.org/x/net/context"
)

// FakeServerClient implements a fake SearchServerInterface.
type FakeServerClient struct {
	docIDs      []sserver1.DocumentID // The list of document IDs added.
	searchCount int                   // The number of times `SearchWord` has been called.  Needed to return the expected results.
}

func (c *FakeServerClient) WriteIndex(_ context.Context, arg sserver1.WriteIndexArg) error {
	c.docIDs = append(c.docIDs, arg.DocID)
	return nil
}

func (c *FakeServerClient) RenameIndex(_ context.Context, arg sserver1.RenameIndexArg) error {
	for i, docID := range c.docIDs {
		if docID == arg.Orig {
			c.docIDs[i] = arg.Curr
		}
	}
	return nil
}

func (c *FakeServerClient) DeleteIndex(_ context.Context, arg sserver1.DeleteIndexArg) error {
	for i, docID := range c.docIDs {
		if docID == arg.DocID {
			c.docIDs = append(c.docIDs[:i], c.docIDs[i+1:]...)
		}
	}
	return nil
}

func (c *FakeServerClient) GetKeyGens(_ context.Context, _ sserver1.FolderID) ([]int, error) {
	return []int{1}, nil
}

func (c *FakeServerClient) SearchWord(_ context.Context, arg sserver1.SearchWordArg) ([]sserver1.DocumentID, error) {
	c.searchCount++
	if c.searchCount == 1 {
		expected := []sserver1.DocumentID{c.docIDs[1], c.docIDs[3]}
		return expected, nil
	} else if c.searchCount == 2 {
		return nil, nil
	} else {
		return c.docIDs, nil
	}
}

func (c *FakeServerClient) RegisterTlfIfNotExists(_ context.Context, _ sserver1.RegisterTlfIfNotExistsArg) (sserver1.TlfInfo, error) {
	return sserver1.TlfInfo{Salts: nil, Size: 10000}, nil
}

// startTestClient creates an instance of a test client and returns a pointer to
// the instance, as well as the name of the client's temporary directory.  Need
// to later manually clean up the directory.  If `dir` is set, initializes the
// client at `dir` instead of creating a temporary directory
func startTestClient(t *testing.T, cliDir string) (*Client, string) {
	var err error
	if cliDir == "" {
		cliDir, err = ioutil.TempDir("", "TestClient")
		if err != nil {
			t.Fatalf("error when creating the test client directory: %s", err)
		}
	}

	var status libkbfs.FolderBranchStatus
	status.FolderID = "aRandomTLFID"
	status.LatestKeyGeneration = 1
	bytes, err := json.MarshalIndent(status, "", "  ")
	if err != nil {
		t.Fatalf("error when writing the TLF status: %s", err)
	}
	err = ioutil.WriteFile(filepath.Join(cliDir, ".kbfs_status"), bytes, 0666)
	if err != nil {
		t.Fatalf("error when writing the TLF status: %s", err)
	}

	searchCli := &FakeServerClient{docIDs: make([]sserver1.DocumentID, 0, 5)}

	cli, err := createClientWithClient(context.Background(), searchCli, []string{cliDir}, 64, 8, 0.000001, 1000)
	if err != nil {
		t.Fatalf("Error when creating the client: %s", err)
	}

	return cli, cliDir
}

// TestCreateClient tests the `CreateClient` function.  Checks that a client can
// be successfully created and that two clients have the same `indexer` and
// `pathnameKey` if created with the same master secret.
func TestCreateClient(t *testing.T) {
	client1, dir := startTestClient(t, "")
	defer os.Remove(dir)
	client2, _ := startTestClient(t, dir)

	if !reflect.DeepEqual(client1.directoryInfos[dir].indexers[0].ComputeTrapdoors("test"), client2.directoryInfos[dir].indexers[0].ComputeTrapdoors("test")) {
		t.Fatalf("clients with different indexer created with the same master secret")
	}
	if client1.directoryInfos[dir].pathnameKeys[0] != client2.directoryInfos[dir].pathnameKeys[0] {
		t.Fatalf("clients with different pathnameKey created with the same master secret")
	}
}

// TestAddFile tests the `AddFile` function.  Checks that the index is properly
// written by the server, and that errors are properly returned when the file is
// not valid.
func TestAddFile(t *testing.T) {
	client, dir := startTestClient(t, "")
	defer os.RemoveAll(dir)

	content := "This is a random test string, it is quite long, or not really"
	if err := ioutil.WriteFile(filepath.Join(dir, "testFile"), []byte(content), 0666); err != nil {
		t.Fatalf("error when writing test file: %s", err)
	}

	if err := client.AddFile(dir, filepath.Join(dir, "testFile")); err != nil {
		t.Fatalf("error when adding the file: %s", err)
	}

	if err := client.AddFile(dir, filepath.Join(dir, "nonExisting")); !os.IsNotExist(err) {
		t.Fatalf("no error returned for non-existing file")
	}

	fileNotInDir, err := ioutil.TempFile("", "tmpFile")
	if err != nil {
		t.Fatalf("error when creating temporary test file: %s", err)
	}
	defer os.Remove(fileNotInDir.Name())

	if err := client.AddFile(dir, fileNotInDir.Name()); err.Error() != "target path not within base path" {
		t.Fatalf("error not properly returned for file not in the client directory")
	}
}

// TestRenameFile tests the `RenameFile` function.  Checks the indexes are
// properly renamed and errors returned when necessary.
func TestRenameFile(t *testing.T) {
	client, dir := startTestClient(t, "")
	defer os.RemoveAll(dir)

	content := "a random content"
	if err := ioutil.WriteFile(filepath.Join(dir, "testRenameFile"), []byte(content), 0666); err != nil {
		t.Fatalf("error when writing test file: %s", err)
	}

	if err := client.AddFile(dir, filepath.Join(dir, "testRenameFile")); err != nil {
		t.Fatalf("error when adding the file: %s", err)
	}

	if err := client.RenameFile(dir, filepath.Join(dir, "testRenameFile"), filepath.Join(dir, "testRename")); err != nil {
		t.Fatalf("error when renaming file: %s", err)
	}

	// Doing the renaming second time should still succeed, even though nothing
	// real has been done.
	if err := client.RenameFile(dir, filepath.Join(dir, "testRenameFile"), filepath.Join(dir, "testRename")); err != nil {
		t.Fatalf("error when renaming a non-existing file: %s", err)
	}
}

// TestDeleteFile tests the `DeleteFile` function.  Checks the indexes are
// properly deleted and errors returned when necessary.
func TestDeleteFile(t *testing.T) {
	client, dir := startTestClient(t, "")
	defer os.RemoveAll(dir)

	content := "a random content"
	if err := ioutil.WriteFile(filepath.Join(dir, "testDeleteFile"), []byte(content), 0666); err != nil {
		t.Fatalf("error when writing test file: %s", err)
	}

	if err := client.AddFile(dir, filepath.Join(dir, "testDeleteFile")); err != nil {
		t.Fatalf("error when adding the file: %s", err)
	}

	if err := client.DeleteFile(dir, filepath.Join(dir, "testDeleteFile")); err != nil {
		t.Fatalf("error when deleting file: %s", err)
	}

	// Doing the deleting second time should still succeed.
	if err := client.DeleteFile(dir, filepath.Join(dir, "testDeleteFile")); err != nil {
		t.Fatalf("error when deleting a non-existing file: %s", err)
	}
}

// testSearchWordHelper tests the provided 'searchFunc' function.  Checks that
// the correct set of filenames are returned.
func testSearchWordHelper(t *testing.T, searchFunc func(*Client, string, string) ([]string, error)) {
	client, dir := startTestClient(t, "")
	defer os.RemoveAll(dir)

	contents := []string{
		"This is a simple test file",
		"This is another test file",
		"This is a different test file",
		"This is yet another test file",
		"This is the last test file",
	}
	filenames := make([]string, len(contents))

	for i, fileContent := range contents {
		filenames[i] = filepath.Join(dir, "testSearchFile"+strconv.Itoa(i))
		if err := ioutil.WriteFile(filenames[i], []byte(fileContent), 0666); err != nil {
			t.Fatalf("error when writing test file: %s", err)
		}
		if err := client.AddFile(dir, filenames[i]); err != nil {
			t.Fatalf("error when adding the file: %s", err)
		}
	}

	expected := []string{filenames[1], filenames[3]}
	sort.Strings(expected)
	actual, err := searchFunc(client, dir, "another")
	if err != nil {
		t.Fatalf("error when searching word: %s", err)
	}
	if !reflect.DeepEqual(expected, actual) {
		t.Fatalf("incorrect search result: expected \"%s\" actual \"%s\"", expected, actual)
	}

	empty, err := searchFunc(client, dir, "non-existing")
	if err != nil {
		t.Fatalf("error when searching word: %s", err)
	}
	if len(empty) > 0 {
		t.Fatalf("filenames found for non-existing word")
	}

	expected = filenames
	sort.Strings(expected)
	actual, err = searchFunc(client, dir, "file")
	if err != nil {
		t.Fatalf("error when searching word: %s", err)
	}
	if !reflect.DeepEqual(expected, actual) {
		t.Fatalf("incorrect search result: expected \"%s\" actual \"%s\"", expected, actual)
	}
}

// searchWordWrapper is the wrapper function for `SearchWord`.
func searchWordWrapper(client *Client, directory string, word string) ([]string, error) {
	return client.SearchWord(directory, word)
}

// searchWordStrictWrapper is the wrapper function for `SearchWordStrict`.
func searchWordStrictWrapper(client *Client, directory, word string) ([]string, error) {
	return client.SearchWordStrict(directory, word)
}

// TestSearchWord tests the 'SearchWord' function.  Checks that the correct set
// of filenames are returned.
func TestSearchWord(t *testing.T) {
	testSearchWordHelper(t, searchWordWrapper)
}

// TestSearchWord tests the 'SearchWordStrict' function.  Checks that the
// correct set of filenames are returned.
func TestSearchWordStrict(t *testing.T) {
	testSearchWordHelper(t, searchWordStrictWrapper)
}
