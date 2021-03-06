// Copyright 2016 Keybase Inc. All rights reserved.
// Use of this source code is governed by a BSD
// license that can be found in the LICENSE file.

package client

import (
	"bytes"
	"encoding/json"
	"io/ioutil"
	"os"
	"path"
	"reflect"
	"search/prototype/server"
	"sort"
	"testing"
)

// createTestServer creates a test server with the params.  Need to manually
// remove the directory in the second return value.
func createTestServer(numClients, lenMS, lenSalt int, fpRate float64, numUniqWords uint64) (*server.Server, string) {
	dir, err := ioutil.TempDir("", "serverTest")
	if err != nil {
		panic("cannot create the temporary test directory")
	}
	s, err2 := server.CreateServer(numClients, lenMS, lenSalt, dir, fpRate, numUniqWords)
	if err2 != nil {
		panic("error in creating the server")
	}
	return s, dir
}

// createTestClient creates a test client with the params.  Need to manually
// remove the directory in the second return value.
func createTestClient(s *server.Server, clientNum int) (*Client, string) {
	dir, err := ioutil.TempDir("", "clientTest")
	if err != nil {
		panic("cannot create the temporary test directory")
	}
	c := CreateClient(s, clientNum, dir)
	return c, dir
}

// createTestFile creates a temporary file with `content` and returns the
// filename as a string.  The file need to be manually removed by the caller.
func createTestFile(content string) string {
	doc, err := ioutil.TempFile("", "testFile")
	if err != nil {
		panic("cannot create the temporary test file")
	}
	if _, err := doc.Write([]byte(content)); err != nil {
		panic("cannot write to the temporary test file")
	}
	return doc.Name()
}

// TestCreateClient tests the `CreateClient` function.  Checks that multiple
// clients created for the server should behave the same regardless of the
// different client numbers, (except for directory).
func TestCreateClient(t *testing.T) {
	s, dir := createTestServer(5, 8, 8, 0.000001, uint64(100000))
	defer os.RemoveAll(dir)

	c1, cliDir1 := createTestClient(s, 0)
	c2, cliDir2 := createTestClient(s, 1)
	defer os.RemoveAll(cliDir1)
	defer os.RemoveAll(cliDir2)

	if c1.server != c2.server {
		t.Fatalf("different servers for the clients")
	}
	if !reflect.DeepEqual(c1.lookupTable, c2.lookupTable) {
		t.Fatalf("different lookup tables for the clients")
	}
	if !reflect.DeepEqual(c1.indexer.ComputeTrapdoors("testing"), c2.indexer.ComputeTrapdoors("testing")) {
		t.Fatalf("different indexers for the clients")
	}
}

// TestAddFile tests the `AddFile` function.  Checks that the lookup tables are
// correctly updated on both the server and the client, and that the file is
// written correctly both on the client and the server and that the index is
// correctly written on the server.
func TestAddFile(t *testing.T) {
	s, dir := createTestServer(5, 8, 8, 0.000001, uint64(100000))
	defer os.RemoveAll(dir)

	c, cliDir := createTestClient(s, 0)
	defer os.RemoveAll(cliDir)

	content := "This is a simple test file"
	file := createTestFile(content)
	_, filename := path.Split(file)
	defer os.Remove(file)

	if c.AddFile(file) != nil {
		t.Fatalf("first time adding file fails")
	}

	if c.AddFile(file) == nil {
		t.Fatalf("same file added twice")
	}

	if c.lookupTable["0"] != filename {
		t.Fatalf("lookup table not set up correctly on the client")
	}
	if c.reverseLookup[filename] != "0" {
		t.Fatalf("reverse lookup table not set up correctly on the client")
	}

	serverLookupTable := make(map[string]string)
	if tableContent, found := s.ReadLookupTable(); found {
		json.Unmarshal(tableContent, &serverLookupTable)
	}
	if serverLookupTable["0"] != filename {
		t.Fatalf("lookup table not set up correctly on the server")
	}

	if actual, err := s.GetFile(0); err != nil || !bytes.Equal(actual, []byte(content)) {
		t.Fatalf("file not written correctly to the server: %s", err)
	}

	if !reflect.DeepEqual(s.SearchWord(c.indexer.ComputeTrapdoors("simple")), []int{0}) {
		t.Fatalf("index file not written correctly to server")
	}

	contentRead, err := ioutil.ReadFile(path.Join(cliDir, filename))
	if err != nil || !bytes.Equal(contentRead, []byte(content)) {
		t.Fatalf("file not correctly written to local client storage")
	}
}

// TestGetFile tests the `getFile` function.  Checks that the correct file
// content is written to the local disk of the client.
func TestGetFile(t *testing.T) {
	s, dir := createTestServer(5, 8, 8, 0.000001, uint64(100000))
	defer os.RemoveAll(dir)

	c, cliDir := createTestClient(s, 0)
	defer os.RemoveAll(cliDir)

	content := "This is a simple test file"
	file := createTestFile(content)
	_, filename := path.Split(file)
	defer os.Remove(file)

	c.AddFile(file)

	c2, cliDir2 := createTestClient(s, 1)
	defer os.RemoveAll(cliDir2)

	if _, err := os.Stat(path.Join(cliDir2, filename)); err == nil {
		t.Fatalf("file already exist before getFile is called")
	}

	err := c2.getFile(0)

	if err != nil {
		t.Fatalf("error when getting the file: %s", err)
	}

	contentRead, err := ioutil.ReadFile(path.Join(cliDir2, filename))
	if err != nil || !bytes.Equal(contentRead, []byte(content)) {
		t.Fatalf("file content not successfully retrieved after getFile")
	}
}

// TestSearchWord tests the `SearchWord` function.  Checks that the expected
// filenames are returned by the function.
func TestSearchWord(t *testing.T) {
	s, dir := createTestServer(5, 8, 8, 0.000001, uint64(100000))
	defer os.RemoveAll(dir)

	c, cliDir := createTestClient(s, 0)
	defer os.RemoveAll(cliDir)

	contents := []string{
		"This is a simple test file",
		"This is another test file",
		"This is a different test file",
		"This is yet another test file",
		"This is the last test file"}

	filenames := make([]string, 5)

	for i := 0; i < len(contents); i++ {
		file := createTestFile(contents[i])
		defer os.Remove(file)
		_, filenames[i] = path.Split(file)
		c.AddFile(file)
	}

	c2, cliDir2 := createTestClient(s, 1)
	defer os.RemoveAll(cliDir2)

	expected := []string{filenames[1], filenames[3]}
	sort.Strings(expected)
	actual, _, err := c2.SearchWord("another")
	if err != nil {
		t.Fatalf("error when searching word: %s", err)
	}
	sort.Strings(actual)
	if !reflect.DeepEqual(expected, actual) {
		t.Fatalf("incorrect search result")
	}

	empty, _, err := c2.SearchWord("non-existing")
	if err != nil {
		t.Fatalf("error when searching word: %s", err)
	}
	if len(empty) > 0 {
		t.Fatalf("filenames found for non-existing word")
	}

	expected = filenames
	sort.Strings(expected)
	actual, _, err = c2.SearchWord("file")
	if err != nil {
		t.Fatalf("error when searching word: %s", err)
	}
	sort.Strings(actual)
	if !reflect.DeepEqual(expected, actual) {
		t.Fatalf("incorrect search result")
	}
}

// TestSearchWordNaive tests the `SearchWordNaive` function.  Checks that the
// expected filenames are returned by the function.
func TestSearchWordNaive(t *testing.T) {
	s, dir := createTestServer(5, 8, 8, 0.000001, uint64(100000))
	defer os.RemoveAll(dir)

	c, cliDir := createTestClient(s, 0)
	defer os.RemoveAll(cliDir)

	contents := []string{
		"This is a simple test file",
		"This is another test file",
		"This is a different test file",
		"This is yet another test file",
		"This is the last test file"}

	filenames := make([]string, 5)

	for i := 0; i < len(contents); i++ {
		file := createTestFile(contents[i])
		defer os.Remove(file)
		_, filenames[i] = path.Split(file)
		c.AddFile(file)
	}

	c2, cliDir2 := createTestClient(s, 1)
	defer os.RemoveAll(cliDir2)

	expected := []string{filenames[1], filenames[3]}
	sort.Strings(expected)
	actual, _, err := c2.SearchWordNaive("another")
	if err != nil {
		t.Fatalf("error when searching word: %s", err)
	}
	sort.Strings(actual)
	if !reflect.DeepEqual(expected, actual) {
		t.Fatalf("incorrect search result")
	}

	empty, _, err := c2.SearchWordNaive("non-existing")
	if err != nil {
		t.Fatalf("error when searching word: %s", err)
	}
	if len(empty) > 0 {
		t.Fatalf("filenames found for non-existing word")
	}

	expected = filenames
	sort.Strings(expected)
	actual, _, err = c2.SearchWordNaive("file")
	if err != nil {
		t.Fatalf("error when searching word: %s", err)
	}
	sort.Strings(actual)
	if !reflect.DeepEqual(expected, actual) {
		t.Fatalf("incorrect search result")
	}

}
