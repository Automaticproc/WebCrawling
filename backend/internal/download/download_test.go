package download

import (
	"context"
	"crypto/aes"
	"crypto/cipher"
	"encoding/hex"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"catalog/internal/store"
	"catalog/internal/store/memstore"
)

func TestParseMasterAndMedia(t *testing.T) {
	master := `#EXTM3U
#EXT-X-STREAM-INF:BANDWIDTH=800000,RESOLUTION=640x360,CODECS="avc1.42e01e"
low/index.m3u8
#EXT-X-STREAM-INF:BANDWIDTH=2000000,RESOLUTION=1280x720
high/index.m3u8
`
	vs, pl, err := ParsePlaylist(master, "https://cdn.example.com/a/master.m3u8")
	if err != nil {
		t.Fatal(err)
	}
	if pl != nil || len(vs) != 2 {
		t.Fatalf("master parse: %d variants", len(vs))
	}
	if vs[1].Height != 720 || vs[1].URL != "https://cdn.example.com/a/high/index.m3u8" {
		t.Fatalf("variant: %+v", vs[1])
	}
	chosen, _ := ChooseVariant(vs, "720p")
	if chosen.Height != 720 {
		t.Fatalf("choose: %+v", chosen)
	}

	media := `#EXTM3U
#EXT-X-VERSION:3
#EXT-X-MEDIA-SEQUENCE:10
#EXT-X-KEY:METHOD=AES-128,URI="key.bin",IV=0x000102030405060708090a0b0c0d0e0f
#EXT-X-MAP:URI="init.mp4"
#EXTINF:4.0,
seg10.m4s
#EXTINF:3.5,
seg11.m4s
#EXT-X-ENDLIST
`
	_, pl, err = ParsePlaylist(media, "https://cdn.example.com/a/high/index.m3u8")
	if err != nil {
		t.Fatal(err)
	}
	if pl == nil || len(pl.Segments) != 2 || !pl.Complete {
		t.Fatalf("media parse: %+v", pl)
	}
	if pl.Segments[0].Key.URI != "https://cdn.example.com/a/high/key.bin" {
		t.Fatalf("key uri: %s", pl.Segments[0].Key.URI)
	}
	if pl.Segments[0].InitURL != "https://cdn.example.com/a/high/init.mp4" {
		t.Fatalf("init: %s", pl.Segments[0].InitURL)
	}
	if pl.Segments[1].Seq != 11 || pl.Duration != 7.5 {
		t.Fatalf("seq/duration: %+v", pl)
	}
}

func TestAESDecrypt(t *testing.T) {
	key, _ := hex.DecodeString("000102030405060708090a0b0c0d0e0f")
	iv, _ := hex.DecodeString("0f0e0d0c0b0a09080706050403020100")
	plain := []byte("hello hls segment")
	pad := aes.BlockSize - len(plain)%aes.BlockSize
	padded := append(append([]byte{}, plain...), bytesRepeat(byte(pad), pad)...)
	block, _ := aes.NewCipher(key)
	enc := make([]byte, len(padded))
	cipher.NewCBCEncrypter(block, iv).CryptBlocks(enc, padded)

	got, err := decryptAES128(enc, key, iv)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != string(plain) {
		t.Fatalf("decrypt: %q", got)
	}
}

func bytesRepeat(b byte, n int) []byte {
	out := make([]byte, n)
	for i := range out {
		out[i] = b
	}
	return out
}

func TestFindPlaylists(t *testing.T) {
	body := []byte(`<script>var u="https:\/\/cdn.example.com\/a\/index.m3u8?token=1";</script>
<video src="/b/play.m3u8"></video>`)
	got := FindPlaylists(body, "https://site.example.com/video/1/")
	if len(got) != 2 {
		t.Fatalf("find: %v", got)
	}
	if got[0] != "https://cdn.example.com/a/index.m3u8?token=1" {
		t.Fatalf("abs: %s", got[0])
	}
	if got[1] != "https://site.example.com/b/play.m3u8" {
		t.Fatalf("rel: %s", got[1])
	}
}

func hlsFixture(t *testing.T) (dir string, ffmpeg string) {
	t.Helper()
	ff, err := exec.LookPath("ffmpeg")
	if err != nil {
		t.Skip("ffmpeg 不可用")
	}
	dir = t.TempDir()
	cmd := exec.Command(ff, "-hide_banner", "-loglevel", "error", "-y",
		"-f", "lavfi", "-i", "testsrc2=size=320x180:rate=15",
		"-t", "4", "-c:v", "libx264", "-preset", "ultrafast", "-g", "30",
		"-f", "hls", "-hls_time", "2", "-hls_playlist_type", "vod",
		"-hls_segment_filename", filepath.Join(dir, "seg_%03d.ts"),
		filepath.Join(dir, "index.m3u8"))
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Skipf("生成测试 HLS 失败：%v %s", err, out)
	}
	return dir, ff
}

func TestEngineDownload(t *testing.T) {
	dir, ff := hlsFixture(t)
	srv := httptest.NewServer(http.FileServer(http.Dir(dir)))
	defer srv.Close()

	engine := NewEngine(ff, 4)
	engine.TempRoot = t.TempDir()
	out := filepath.Join(t.TempDir(), "out.mp4")
	res, err := engine.Download(context.Background(), srv.URL+"/index.m3u8", nil, out, "best", nil)
	if err != nil {
		t.Fatal(err)
	}
	if res.Size <= 0 || res.Segments < 1 {
		t.Fatalf("result: %+v", res)
	}
	if st, err := os.Stat(out); err != nil || st.Size() == 0 {
		t.Fatalf("output missing: %v", err)
	}
}

func TestRetryOnlyFailedEpisodes(t *testing.T) {
	st := memstore.New()
	ctx := context.Background()
	p, err := st.CreatePlatform(ctx, store.Platform{Code: "demo", Name: "Demo", BaseURL: "https://example.com"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := st.UpsertWork(ctx, store.Work{
		PlatformID: p.ID, ExternalID: "1", Title: "重试测试", PlayURL: "https://example.com/video/1/",
		EpisodeCount: 3,
	}); err != nil {
		t.Fatal(err)
	}
	list, _, err := st.ListWorks(ctx, store.WorkFilter{})
	if err != nil || len(list) != 1 {
		t.Fatalf("works: %v", err)
	}
	work := list[0]

	task, err := st.CreateDownloadTask(ctx, store.DownloadTask{
		WorkID: work.ID, PlatformID: p.ID, Status: store.DownloadFailed,
		TotalEpisodes: 3, FailedEpisodes: 2, Quality: "best", OutputDir: t.TempDir(),
	})
	if err != nil {
		t.Fatal(err)
	}
	done := filepath.Join(task.OutputDir, "第01集.mp4")
	if err := os.WriteFile(done, []byte("ok"), 0o644); err != nil {
		t.Fatal(err)
	}
	items := []store.DownloadItem{
		{TaskID: task.ID, EpisodeNo: 1, Title: "第1集", PageURL: "http://127.0.0.1:1/", Status: store.DownloadSucceeded, FilePath: done, SizeBytes: 2},
		{TaskID: task.ID, EpisodeNo: 2, Title: "第2集", PageURL: "http://127.0.0.1:1/", Status: store.DownloadFailed},
		{TaskID: task.ID, EpisodeNo: 3, Title: "第3集", PageURL: "http://127.0.0.1:1/", Status: store.DownloadFailed},
	}
	for _, it := range items {
		if _, err := st.AddDownloadItem(ctx, it); err != nil {
			t.Fatal(err)
		}
	}

	engine := NewEngine("ffmpeg", 2)
	engine.TempRoot = t.TempDir()
	resolver := NewResolver("", time.Second)
	resolver.Client.Timeout = 500 * time.Millisecond
	svc := NewService(st, resolver, engine, t.TempDir())
	svc.Delay = 0

	retry, err := svc.RetryTask(ctx, task.ID)
	if err != nil {
		t.Fatal(err)
	}
	if retry.TotalEpisodes != 2 || len(retry.Items) != 2 {
		t.Fatalf("retry task: total=%d items=%d", retry.TotalEpisodes, len(retry.Items))
	}
	if retry.Items[0].EpisodeNo != 2 || retry.Items[1].EpisodeNo != 3 {
		t.Fatalf("episodes: %+v", retry.Items)
	}
	_ = svc.Cancel(ctx, retry.ID)

	// 全部完成且文件还在时，没有可重试的集（换一部作品，避免和上面的重试任务抢锁）
	if _, err := st.UpsertWork(ctx, store.Work{
		PlatformID: p.ID, ExternalID: "2", Title: "重试测试2", PlayURL: "https://example.com/video/2/", EpisodeCount: 1,
	}); err != nil {
		t.Fatal(err)
	}
	all, _, err := st.ListWorks(ctx, store.WorkFilter{})
	if err != nil {
		t.Fatal(err)
	}
	var work2 store.Work
	for _, w := range all {
		if w.ExternalID == "2" {
			work2 = w
		}
	}
	if work2.ID == 0 {
		t.Fatal("work2 missing")
	}
	allDone := t.TempDir()
	task2, err := st.CreateDownloadTask(ctx, store.DownloadTask{
		WorkID: work2.ID, PlatformID: p.ID, Status: store.DownloadSucceeded,
		TotalEpisodes: 1, CompletedEpisodes: 1, Quality: "best", OutputDir: allDone,
	})
	if err != nil {
		t.Fatal(err)
	}
	file := filepath.Join(allDone, "第01集.mp4")
	if err := os.WriteFile(file, []byte("ok"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := st.AddDownloadItem(ctx, store.DownloadItem{
		TaskID: task2.ID, EpisodeNo: 1, Title: "第1集", Status: store.DownloadSucceeded, FilePath: file, SizeBytes: 2,
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.RetryTask(ctx, task2.ID); !errors.Is(err, ErrNothingToRetry) {
		t.Fatalf("want ErrNothingToRetry, got %v", err)
	}
}

func TestServiceDownloadsAllEpisodes(t *testing.T) {
	dir, ff := hlsFixture(t)
	srv := httptest.NewServer(http.FileServer(http.Dir(dir)))
	defer srv.Close()

	page := `<!doctype html><html><body><script>
	var u = "` + srv.URL + `/index.m3u8";
	</script></body></html>`
	pageSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(page))
	}))
	defer pageSrv.Close()

	st := memstore.New()
	ctx := context.Background()
	p, err := st.CreatePlatform(ctx, store.Platform{Code: "demo", Name: "Demo", BaseURL: pageSrv.URL})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := st.UpsertWork(ctx, store.Work{
		PlatformID: p.ID, ExternalID: "1", Title: "测试剧", PlayURL: pageSrv.URL + "/video/1/",
		EpisodeCount: 2, Episodes: []store.Episode{
			{EpisodeNo: 1, Title: "第1集", PlayURL: pageSrv.URL + "/video/1/"},
			{EpisodeNo: 2, Title: "第2集", PlayURL: pageSrv.URL + "/video/1/ep-2/"},
		},
	}); err != nil {
		t.Fatal(err)
	}
	list, _, err := st.ListWorks(ctx, store.WorkFilter{})
	if err != nil || len(list) != 1 {
		t.Fatalf("works: %v %v", err, list)
	}
	w := list[0]
	if w.ID == 0 {
		t.Fatal("work id missing")
	}

	engine := NewEngine(ff, 4)
	engine.TempRoot = t.TempDir()
	outRoot := t.TempDir()
	svc := NewService(st, NewResolver("", time.Second), engine, outRoot)
	svc.Delay = 0
	task, err := svc.StartWork(ctx, w.ID, "best")
	if err != nil {
		t.Fatal(err)
	}

	deadline := time.Now().Add(90 * time.Second)
	for time.Now().Before(deadline) {
		got, err := st.GetDownloadTask(ctx, task.ID)
		if err != nil {
			t.Fatal(err)
		}
		if got.Status == store.DownloadSucceeded || got.Status == store.DownloadFailed {
			if got.Status != store.DownloadSucceeded {
				t.Fatalf("task status=%s err=%s", got.Status, got.ErrorMessage)
			}
			if got.CompletedEpisodes != 2 {
				t.Fatalf("completed=%d", got.CompletedEpisodes)
			}
			for _, it := range got.Items {
				if !strings.HasSuffix(it.FilePath, ".mp4") {
					t.Fatalf("item path: %s", it.FilePath)
				}
				if st, err := os.Stat(it.FilePath); err != nil || st.Size() == 0 {
					t.Fatalf("file missing: %v", err)
				}
			}
			return
		}
		time.Sleep(250 * time.Millisecond)
	}
	t.Fatalf("task did not finish")
}
