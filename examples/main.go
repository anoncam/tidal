package main

import (
	"fmt"
	"io"
	"math"
	"net/http"
	"os"
	"strings"

	tui "github.com/marcusolsson/tui-go"
	"github.com/mewkiz/flac"
	"github.com/mewkiz/flac/meta"
	"github.com/the5heepdev/tidal"
)

var t *tidal.Tidal
var albumResults []tidal.Album
var trackResults []tidal.Track
var ui tui.UI
var status *tui.StatusBar
var progress *tui.Progress
var infoBox *tui.Box
var downQueue = make(chan tidal.Track, 512)
var downList []tidal.Track
var todo, done int

func main() {
	t = tidal.New("", "") // input your username and password
	win := tui.NewTable(0, 0)
	win.SetColumnStretch(0, 2)
	win.SetColumnStretch(1, 4)
	win.SetColumnStretch(2, 4)
	win.SetColumnStretch(3, 1)
	win.SetColumnStretch(4, 1)
	win.SetColumnStretch(5, 1)
	win.SetSizePolicy(tui.Maximum, tui.Maximum)
	win.AppendRow(
		tui.NewLabel("ARTIST"),
		tui.NewLabel("ALBUM"),
		tui.NewLabel("TITLE"),
		tui.NewLabel("NUMBER"),
		tui.NewLabel("EXPLICIT"),
		tui.NewLabel(""),
	)
	win.SetSelected(1)
	libBox := tui.NewVBox(
		win,
		tui.NewSpacer(),
	)
	libBox.SetBorder(true)
	libBox.SetTitle("=[   Results   ]=")

	dl := tui.NewList()
	dlS := tui.NewScrollArea(dl)
	dlBox := tui.NewVBox(dlS)
	dlBox.SetBorder(true)
	dlBox.SetTitle("=[  Downloads  ]=")

	input := tui.NewEntry()
	input.SetFocused(true)
	input.SetSizePolicy(tui.Expanding, tui.Maximum)
	inputBox := tui.NewVBox(input)
	inputBox.SetBorder(true)
	inputBox.SetSizePolicy(tui.Expanding, tui.Maximum)
	inputBox.SetTitle("=[Search Tracks]=")
	searchType := 0

	progress = tui.NewProgress(100)
	progress.SetSizePolicy(tui.Expanding, tui.Maximum)
	infoBox = tui.NewHBox(progress)
	infoBox.SetBorder(true)
	infoBox.SetSizePolicy(tui.Expanding, tui.Maximum)
	infoBox.SetTitle("=[ 0 | 0 ][]=")

	help := tui.NewLabel("[Tab: Switch Search Type]   [ShiftTab: Switch View]   [CtrlQ: Quit]")
	help.SetSizePolicy(tui.Expanding, tui.Maximum)

	v := []*tui.Box{
		tui.NewVBox(
			infoBox,
			libBox,
			inputBox,
			help,
		),
		tui.NewVBox(
			infoBox,
			dlBox,
			help,
		),
	}

	cur := 0

	ui = tui.New(v[0])
	ui.SetKeybinding("Ctrl+Q", func() { ui.Quit() })

	ui.SetKeybinding("Enter", func() {
		if cur != 1 {
			if input.Text() != "" {
				win.RemoveRows()
				win.AppendRow(
					tui.NewLabel("ARTIST"),
					tui.NewLabel("ALBUM"),
					tui.NewLabel("TITLE"),
					tui.NewLabel("NUMBER"),
					tui.NewLabel("EXPLICIT"),
					tui.NewLabel(""),
				)
				win.SetSelected(1)
				switch searchType {
				case 0:
					trackResults = t.SearchTracks(input.Text(), fmt.Sprintf("%d", libBox.Size().Y))
					for _, v := range trackResults {
						win.AppendRow(
							tui.NewLabel(v.Artists[0].Name),
							tui.NewLabel(v.Album.Title),
							tui.NewLabel(v.Title),
							tui.NewLabel(v.TrackNumber.String()),
							tui.NewLabel(fmt.Sprintf("%t", v.Explicit)),
						)
					}

				case 1:
					albumResults = t.SearchAlbums(input.Text(), fmt.Sprintf("%d", libBox.Size().Y))
					for _, v := range albumResults {
						win.AppendRow(
							tui.NewLabel(v.Artists[0].Name),
							tui.NewLabel(v.Title),
							tui.NewLabel(""),
							tui.NewLabel(v.NumberOfTracks.String()),
							tui.NewLabel(fmt.Sprintf("%t", v.Explicit)),
						)
					}
				}
				input.SetText("")
			} else if len(albumResults) > 0 || len(trackResults) > 0 {
				go func() {
					switch searchType {
					case 0:
						todo++
						v := trackResults[win.Selected()-1]
						dl.AddItems(fmt.Sprintf("%s - %s", v.Artists[0].Name, v.Title))
						downQueue <- v
					case 1:
						d := t.GetAlbumTracks(albumResults[win.Selected()-1].ID.String())
						todo += len(d)
						for _, v := range d {
							dl.AddItems(fmt.Sprintf("%s - %s", v.Artists[0].Name, v.Title))
							downQueue <- v
						}
					}
				}()
			}
		}
	})
	ui.SetKeybinding("Up", func() {
		if cur == 1 {
			dlS.Scroll(0, -1)
		}
	})
	ui.SetKeybinding("Down", func() {
		if cur == 1 {
			dlS.Scroll(0, 1)
		}
	})
	ui.SetKeybinding("PgUp", func() {
		if cur == 0 {
			win.Select(win.Selected() - 10)
		} else {
			dlS.Scroll(0, -10)
		}
	})
	ui.SetKeybinding("PgDn", func() {
		if cur == 0 {
			win.Select(win.Selected() + 10)
		} else {
			dlS.Scroll(0, 10)
		}
	})
	ui.SetKeybinding("BackTab", func() {
		cur = int(math.Abs(float64(cur - 1))) // toggle between
		ui.SetWidget(v[cur])
	})
	ui.SetKeybinding("Tab", func() {
		searchType = int(math.Abs(float64(searchType - 1))) // toggle between
		if searchType == 0 {
			inputBox.SetTitle("=[Search Tracks]=")
		} else {
			inputBox.SetTitle("=[Search Albums]=")
		}
	})

	win.OnSelectionChanged(func(t *tui.Table) {
		if t.Selected() >= win.Size().Y {
			t.SetSelected(win.Size().Y - 1)
		} else if t.Selected() < 1 {
			t.SetSelected(1)
		}
	})

	go func() {
		for v := range downQueue {
			done++
			downloadTrack(v, "LOSSLESS")
		}
	}()

	if err := ui.Run(); err != nil {
		panic(err)
	}
}

// DownloadTrack (id of track, quality of file)
func downloadTrack(tr tidal.Track, q string) {
	dirs := clean(tr.Artists[0].Name) + "/" + clean(tr.Album.Title)
	path := dirs + "/" + clean(tr.Artists[0].Name) + " - " + clean(tr.Title)
	os.MkdirAll(dirs, os.ModePerm)
	f, err := os.Create(path)
	if err != nil {
		fmt.Println(err)
		return
	}

	u := t.GetStreamURL(tr.ID.String(), q)
	res, err := http.Get(u)
	if err != nil {
		fmt.Println(err)
		return
	}

	ui.Update(func() { infoBox.SetTitle(fmt.Sprintf("=[ %d | %d ][ %s ]=", done, todo, path)) })
	r := newProxy(res.Body, int(res.ContentLength), path)
	io.Copy(f, r)
	f.Close()
	r.Close()

	err = enc(path, tr.Title, tr.Artists[0].Name, tr.Album.Title, tr.TrackNumber.String())
	if err != nil {
		fmt.Println(err)
	}
}

func clean(s string) string {
	return strings.Replace(s, "/", "\u2215", -1)
}

func enc(src, title, artist, album, num string) error {
	// Decode FLAC file.
	stream, err := flac.ParseFile(src)
	if err != nil {
		return err
	}
	defer stream.Close()

	// Add custom vorbis comment.
	for _, block := range stream.Blocks {
		if comment, ok := block.Body.(*meta.VorbisComment); ok {
			comment.Tags = append(comment.Tags, [2]string{"TITLE", title})
			comment.Tags = append(comment.Tags, [2]string{"ARTIST", artist})
			comment.Tags = append(comment.Tags, [2]string{"ALBUMARTIST", artist})
			comment.Tags = append(comment.Tags, [2]string{"ALBUM", album})
			comment.Tags = append(comment.Tags, [2]string{"TRACKNUMBER", num})
		}
	}

	// Encode FLAC file.
	f, err := os.Create(src + ".flac")
	if err != nil {
		return err
	}
	defer f.Close()
	err = flac.Encode(f, stream)
	if err != nil {
		return err
	}
	return os.Remove(src)
}

/* little bit to proxy the reader and update the progress bar */
type proxyRead struct {
	io.Reader
	t, l int
	p    string
}

func newProxy(r io.Reader, l int, p string) *proxyRead {
	return &proxyRead{r, 0, l, p}
}

func (r *proxyRead) Read(p []byte) (n int, err error) {
	n, err = r.Reader.Read(p)
	r.t += n
	ui.Update(func() {
		progress.SetCurrent(r.t)
		progress.SetMax(r.l)
		infoBox.SetTitle(fmt.Sprintf("=[ (%d/%d) %s ]=", done, todo, r.p))
	})
	return
}

// Close the reader when it implements io.Closer
func (r *proxyRead) Close() (err error) {
	if closer, ok := r.Reader.(io.Closer); ok {
		ui.Update(func() {
			progress.SetCurrent(0)
			progress.SetMax(1)
		})
		return closer.Close()
	}
	return
}
