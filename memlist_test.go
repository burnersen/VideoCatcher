// VideoCatcher (https://github.com/burnersen/VideoCatcher)
// Copyright (C) 2026 burnersen
// SPDX-License-Identifier: GPL-3.0-only

package main

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"log/slog"
	"net/url"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

// listFiles gives writeList a folder to write into, like intake.writeSecret.
func listFiles(t *testing.T) (dir string, write func(pattern, text string) (string, error)) {
	t.Helper()
	dir = t.TempDir()
	return dir, func(pattern, text string) (string, error) {
		f, err := os.CreateTemp(dir, pattern)
		if err != nil {
			return "", err
		}
		defer f.Close()
		_, err = f.WriteString(text)
		return f.Name(), err
	}
}

func dataLink(text string) string {
	return "data:application/vnd.apple.mpegurl;base64," + base64.StdEncoding.EncodeToString([]byte(text))
}

func readFileURL(t *testing.T, link string) string {
	t.Helper()
	u, err := url.Parse(link)
	if err != nil || u.Scheme != "file" {
		t.Fatalf("not a file link: %q", link)
	}
	data, err := os.ReadFile(filepath.FromSlash(u.Path))
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

// A list of qualities with each quality written into it, as a player
// builds it in memory: one file per list, the parts stay web links, links
// without a host are read against the player's page.
func TestWriteListOfQualities(t *testing.T) {
	dir, write := listFiles(t)
	quality := "#EXTM3U\n#EXT-X-TARGETDURATION:6\n#EXT-X-KEY:METHOD=AES-128,URI=\"/player/key.php?id=1\",IV=0x01\n" +
		"#EXTINF:6.0,\nhttps://cdn.example/v/1080_enc/1.html\n#EXTINF:6.0,\nseg/2.html\n#EXT-X-ENDLIST\n"
	master := "\uFEFF#EXTM3U\r\n#EXT-X-STREAM-INF:BANDWIDTH=5000000,RESOLUTION=1920x1080\r\n" + dataLink(quality) + "\r\n" +
		"#EXT-X-STREAM-INF:BANDWIDTH=800000,RESOLUTION=854x480\r\nhttps://cdn.example/480/index.m3u8\r\n"
	files, err := writeList(master, "https://player.example/player/index.php?data=1", write)
	if err != nil {
		t.Fatal(err)
	}
	if len(files) != 2 {
		t.Fatalf("files: %v", files)
	}
	for _, f := range files {
		if filepath.Dir(f) != dir {
			t.Errorf("%s is outside the folder", f)
		}
	}
	main, err := os.ReadFile(files[0])
	if err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(string(main), "\n")
	if !strings.Contains(lines[0], "#EXTM3U") || lines[2] != fileURL(files[1]) || lines[4] != "https://cdn.example/480/index.m3u8" {
		t.Errorf("list of qualities:\n%s", main)
	}
	sub := readFileURL(t, lines[2])
	for _, want := range []string{
		`URI="https://player.example/player/key.php?id=1",IV=0x01`,
		"\nhttps://cdn.example/v/1080_enc/1.html\n",
		"\nhttps://player.example/player/seg/2.html\n",
	} {
		if !strings.Contains(sub, want) {
			t.Errorf("quality lacks %q:\n%s", want, sub)
		}
	}
}

// A key written into the list goes into a file of its own.
func TestWriteListWithKeyInIt(t *testing.T) {
	_, write := listFiles(t)
	key := []byte("0123456789abcdef")
	list := "#EXTM3U\n#EXT-X-KEY:METHOD=AES-128,URI=\"data:application/octet-stream;base64," + base64.StdEncoding.EncodeToString(key) + "\"\n" +
		"#EXTINF:6.0,\nhttps://cdn.example/1.ts\n#EXT-X-ENDLIST\n"
	files, err := writeList(list, "https://player.example/e/1", write)
	if err != nil || len(files) != 2 {
		t.Fatalf("%v %v", files, err)
	}
	main, _ := os.ReadFile(files[0])
	if !strings.Contains(string(main), `URI="`+fileURL(files[1])+`"`) {
		t.Errorf("list:\n%s", main)
	}
	if got, _ := os.ReadFile(files[1]); string(got) != string(key) {
		t.Errorf("key file: %q", got)
	}
}

// Nothing but web links and the job's own files comes out: a list that
// names a file of this PC, another protocol or a key in the player's
// memory is refused as a whole.
func TestWriteListRefuses(t *testing.T) {
	part := "#EXTINF:6.0,\nhttps://cdn.example/1.ts\n"
	for name, c := range map[string]struct {
		list, base string
		key        bool
	}{
		"a file of this PC":       {"#EXTM3U\n#EXTINF:6.0,\nfile:///C:/Users/me/secret.txt\n", "https://p.example/", false},
		"a file as a key":         {"#EXTM3U\n#EXT-X-KEY:METHOD=AES-128,URI=\"file:///etc/passwd\"\n" + part, "https://p.example/", true},
		"a file as the start":     {"#EXTM3U\n#EXT-X-MAP:URI=\"file:///etc/passwd\"\n" + part, "https://p.example/", false},
		"a share in the network":  {"#EXTM3U\n#EXTINF:6.0,\n\\\\server\\share\\a.ts\n", "file:///C:/x", false},
		"another protocol":        {"#EXTM3U\n#EXTINF:6.0,\nftp://cdn.example/1.ts\n", "https://p.example/", false},
		"a part from memory":      {"#EXTM3U\n#EXTINF:6.0,\nblob:https://p.example/1-2\n", "https://p.example/", false},
		"a key from memory":       {"#EXTM3U\n#EXT-X-KEY:METHOD=AES-128,URI=\"blob:https://p.example/1-2\"\n" + part, "https://p.example/", true},
		"a part as data":          {"#EXTM3U\n#EXTINF:6.0,\ndata:video/mp2t;base64,AAAA\n", "https://p.example/", false},
		"a list inside a list":    {"#EXTM3U\n#EXT-X-STREAM-INF:BANDWIDTH=1\n" + dataLink("#EXTM3U\n#EXT-X-STREAM-INF:BANDWIDTH=1\n"+dataLink("#EXTM3U\n"+part)+"\n") + "\n", "https://p.example/", false},
		"a file inside a quality": {"#EXTM3U\n#EXT-X-STREAM-INF:BANDWIDTH=1\n" + dataLink("#EXTM3U\n#EXTINF:6.0,\nfile:///C:/secret\n") + "\n", "https://p.example/", false},
		"too long a key":          {"#EXTM3U\n#EXT-X-KEY:METHOD=AES-128,URI=\"data:;base64," + base64.StdEncoding.EncodeToString(make([]byte, 100)) + "\"\n" + part, "https://p.example/", false},
		"no list":                 {"<html>#EXTM3U</html>", "https://p.example/", false},
		"no page":                 {"#EXTM3U\n" + part, "", false},
		"too big":                 {"#EXTM3U\n" + strings.Repeat(part, maxListBytes/len(part)+1), "https://p.example/", false},
	} {
		_, write := listFiles(t)
		files, err := writeList(c.list, c.base, write)
		if err == nil {
			main, _ := os.ReadFile(files[0])
			t.Errorf("%s: accepted:\n%s", name, main)
			continue
		}
		if errors.Is(err, errListKey) != c.key {
			t.Errorf("%s: %v", name, err)
		}
	}
	// More lists than one video has.
	_, write := listFiles(t)
	var many strings.Builder
	many.WriteString("#EXTM3U\n")
	for i := 0; i <= maxListFiles; i++ {
		many.WriteString("#EXT-X-STREAM-INF:BANDWIDTH=1\n" + dataLink("#EXTM3U\n"+part) + "\n")
	}
	if _, err := writeList(many.String(), "https://p.example/", write); err == nil {
		t.Error("a list with too many lists in it was accepted")
	}
}

func TestFileURL(t *testing.T) {
	for in, want := range map[string]string{
		"/tmp/a b/l-1.m3u8":  "file:///tmp/a%20b/l-1.m3u8",
		"C:/Users/me/l.m3u8": "file:///C:/Users/me/l.m3u8",
	} {
		if got := fileURL(in); got != want {
			t.Errorf("%s: %s", in, got)
		}
	}
}

// The job of a list from memory: yt-dlp gets the file and may read file
// links, the list shows the player's page, and the files go when the job
// ends. No path of this user's folder reaches the details.
func TestListFromMemoryRuns(t *testing.T) {
	var runs [][]string
	r := &stepRunner{steps: []func([]string) ([]string, error){
		func(args []string) ([]string, error) {
			runs = append(runs, args)
			return []string{"ERROR: [generic] Unable to download " + linkOf(args) + ": HTTP Error 404: Not Found"}, errors.New("exit code 1")
		},
	}}
	q, _ := newTestQueue(t, r)
	if err := os.MkdirAll(q.cookieDir, 0o700); err != nil {
		t.Fatal(err)
	}
	files, err := writeList("#EXTM3U\n#EXT-X-STREAM-INF:BANDWIDTH=1\n"+dataLink("#EXTM3U\n#EXTINF:6.0,\nhttps://cdn.example/1.ts\n#EXT-X-ENDLIST\n")+"\n",
		"https://player.example/e/1", func(pattern, text string) (string, error) {
			f, err := os.CreateTemp(q.cookieDir, pattern)
			if err != nil {
				return "", err
			}
			defer f.Close()
			_, err = f.WriteString(text)
			return f.Name(), err
		})
	if err != nil {
		t.Fatal(err)
	}
	j, err := q.AddWith("https://player.example/e/1", testOptions(t), "extension", "https://player.example/e/1", "T", "", jobExtras{ListFiles: files})
	if err != nil {
		t.Fatal(err)
	}
	if len(j.ListNames) != 2 || j.URL != "https://player.example/e/1" {
		t.Fatalf("%+v", j)
	}
	waitFor(t, "failed", func() bool { return jobByID(q, j.ID).Status == jobFailed })
	if len(runs) != 1 || linkOf(runs[0]) != fileURL(files[0]) || !slices.Contains(runs[0], "--enable-file-urls") {
		t.Errorf("runs: %v", runs)
	}
	got := jobByID(q, j.ID)
	if d := strings.Join(got.Details, "\n"); strings.Contains(d, q.cookieDir) || strings.Contains(d, filepath.ToSlash(q.cookieDir)) || !strings.Contains(d, "list/") {
		t.Errorf("details: %s", d)
	}
	for _, f := range files {
		if fileExists(f) {
			t.Errorf("%s stays after the job ended", f)
		}
	}
	if len(got.ListNames) != 0 {
		t.Errorf("%+v", got.ListNames)
	}
	// A link without a list never lets yt-dlp read files.
	if a := buildArgs(testOptions(t), "https://a.example/", q.ts, q.have, jobPaths{}, "", ""); slices.Contains(a, "--enable-file-urls") {
		t.Error("file links are on without a list")
	}
}

// From the request file to the files for yt-dlp: a good list is written
// into the cookie folder, a refused one leaves nothing behind and comes
// with the reason for the window.
func TestRequestWithListFromMemory(t *testing.T) {
	in := newIntake(t.TempDir(), slog.New(slog.DiscardHandler))
	cookies := "# Netscape HTTP Cookie File\n.player.example\tTRUE\t/\tTRUE\t0\tcf_clearance\tX\n"
	good := "#EXTM3U\n#EXT-X-STREAM-INF:BANDWIDTH=1\n" + dataLink("#EXTM3U\n#EXTINF:6.0,\nhttps://cdn.example/1.ts\n#EXT-X-ENDLIST\n") + "\n"
	writeRequest(t, in, "abcdef12-0001.json", map[string]any{"v": 1, "url": "https://player.example/e/1", "source": "extension", "list": good, "cookies": cookies})
	got := collect(in, "--request", "abcdef12-0001.json")
	if len(got) != 1 || got[0].Problem != nil || len(got[0].ListFiles) != 2 || got[0].List != "" || got[0].CookieFile == "" {
		t.Fatalf("%+v", got)
	}
	for _, f := range got[0].ListFiles {
		if filepath.Dir(f) != in.cookies || !fileExists(f) {
			t.Errorf("list file %s", f)
		}
	}
	if raw, err := json.Marshal(got[0]); err != nil || strings.Contains(string(raw), "EXTM3U") {
		t.Errorf("the request still holds the list: %s %v", raw, err)
	}

	for name, list := range map[string]string{
		"a file of this PC": "#EXTM3U\n#EXT-X-STREAM-INF:BANDWIDTH=1\n" + dataLink("#EXTM3U\n#EXTINF:6.0,\nfile:///C:/secret\n") + "\n",
		"a key from memory": "#EXTM3U\n#EXT-X-KEY:METHOD=AES-128,URI=\"blob:https://player.example/1\"\n#EXTINF:6.0,\nhttps://cdn.example/1.ts\n",
	} {
		before, _ := os.ReadDir(in.cookies)
		writeRequest(t, in, "abcdef12-0002.json", map[string]any{"v": 1, "url": "https://player.example/e/1", "source": "extension", "list": list, "cookies": cookies})
		got := collect(in, "--request", "abcdef12-0002.json")
		var ue *UserError
		if len(got) != 1 || !errors.As(got[0].Problem, &ue) || len(got[0].ListFiles) != 0 || got[0].CookieFile != "" {
			t.Fatalf("%s: %+v", name, got)
		}
		if (name == "a key from memory") != strings.Contains(ue.Msg, "key") {
			t.Errorf("%s: %s", name, ue.Msg)
		}
		if after, _ := os.ReadDir(in.cookies); len(after) != len(before) {
			t.Errorf("%s: %d files left behind", name, len(after)-len(before))
		}
	}
}
