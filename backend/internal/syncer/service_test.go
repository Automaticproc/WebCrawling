package syncer

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"catalog/internal/store"
	"catalog/internal/store/memstore"
	"catalog/internal/timeutil"
)

func TestSyncSkipAbsentAndMetrics(t *testing.T) {
	var hits []string
	var mu sync.Mutex
	likes := 100
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		hits = append(hits, r.URL.RequestURI())
		mu.Unlock()
		if r.URL.RawQuery != "" || strings.Contains(r.URL.Path, "/api/") {
			http.Error(w, "no", http.StatusForbidden)
			return
		}
		switch r.URL.Path {
		case "/sitemap.xml":
			fmt.Fprintf(w, `<?xml version="1.0" encoding="UTF-8"?>
<sitemapindex xmlns="http://www.sitemaps.org/schemas/sitemap/0.9">
  <sitemap><loc>http://%s/sitemap/video/1.xml</loc></sitemap>
  <sitemap><loc>http://%s/api/secret.xml</loc></sitemap>
  <sitemap><loc>http://%s/stream/a.m3u8</loc></sitemap>
</sitemapindex>`, r.Host, r.Host, r.Host)
		case "/sitemap/video/1.xml":
			fmt.Fprintf(w, `<?xml version="1.0" encoding="UTF-8"?>
<urlset xmlns="http://www.sitemaps.org/schemas/sitemap/0.9">
  <url><loc>http://%s/video/12/</loc><lastmod>2026-11-17</lastmod></url>
  <url><loc>http://%s/video/13/</loc><lastmod>2026-11-17</lastmod></url>
  <url><loc>http://%s/video/14/</loc><lastmod>2026-11-10</lastmod></url>
  <url><loc>http://%s/api/video/15</loc><lastmod>2026-11-17</lastmod></url>
  <url><loc>http://%s/stream/b.m3u8</loc><lastmod>2026-11-17</lastmod></url>
</urlset>`, r.Host, r.Host, r.Host, r.Host, r.Host)
		case "/video/12/":
			body, _ := os.ReadFile("../adapter/huangguoai/testdata/detail_ok.html")
			page := strings.ReplaceAll(string(body), `"likesNum": 2150`, fmt.Sprintf(`"likesNum": %d`, likes))
			_, _ = w.Write([]byte(page))
		case "/video/13/":
			http.ServeFile(w, r, "../adapter/huangguoai/testdata/detail_minor.html")
		case "/video/14/":
			http.ServeFile(w, r, "../adapter/huangguoai/testdata/detail_plain.html")
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	st := memstore.New()
	ctx := context.Background()
	p, err := st.CreatePlatform(ctx, store.Platform{
		Code: "huangguoai", Name: "测试平台", BaseURL: srv.URL, Enabled: true,
		CronExpr: timeutil.DefaultCron, SitemapPath: "/sitemap.xml",
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := st.UpsertWork(ctx, store.Work{
		PlatformID: p.ID, ExternalID: "99", Title: "已下架样本", PlayURL: srv.URL + "/video/99/", Description: "旧简介",
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := st.UpsertWork(ctx, store.Work{
		PlatformID: p.ID, ExternalID: "13", Title: "历史样本", PlayURL: srv.URL + "/video/13/",
		Description: "SYNOPSIS_SENTINEL_MINOR_XYZ 历史简介",
	}); err != nil {
		t.Fatal(err)
	}

	logs := &bufLog{}
	svc := New(st, 0)
	svc.Log = logs
	fixed := time.Date(2026, 11, 17, 12, 0, 0, 0, timeutil.HKT)
	svc.Now = func() time.Time { return fixed }

	job, err := svc.Start(ctx, p.ID, store.ModeFull, store.TriggerManual, true)
	if err != nil {
		t.Fatal(err)
	}
	if job.Status != store.StatusSucceeded {
		t.Fatalf("status %s err %s", job.Status, job.ErrorMessage)
	}
	if job.DiscoveredCount != 3 || job.SkippedCount != 1 || job.AbsentCount != 1 {
		t.Fatalf("counts %+v", job)
	}
	works, _, err := st.ListWorks(ctx, store.WorkFilter{Page: 1, PageSize: 50})
	if err != nil {
		t.Fatal(err)
	}
	byExt := map[string]store.Work{}
	for _, w := range works {
		full, err := st.GetWork(ctx, w.ID)
		if err != nil {
			t.Fatal(err)
		}
		byExt[full.ExternalID] = full
	}
	if _, ok := byExt["13"]; ok {
		t.Fatal("minors work was stored")
	}
	w12 := byExt["12"]
	if w12.Description == "" || w12.LikeCount != 100 || w12.ViewCount != 15000 || w12.AbsentCount != 0 {
		t.Fatalf("work 12 %+v", w12)
	}
	if len(w12.Episodes) != 3 || !strings.HasSuffix(w12.Episodes[1].PlayURL, "/video/12/ep-2/") {
		t.Fatalf("episodes %+v", w12.Episodes)
	}
	if byExt["14"].Description != "普通简介乙" {
		t.Fatalf("work 14 %+v", byExt["14"])
	}
	if byExt["99"].AbsentCount != 1 {
		t.Fatalf("absent %d", byExt["99"].AbsentCount)
	}
	snap := st.TextSnapshot()
	if strings.Contains(snap, "SYNOPSIS_SENTINEL_MINOR_XYZ") || strings.Contains(snap, ".m3u8") || strings.Contains(snap, "SENTINEL_KEY") || strings.Contains(snap, "/api/") {
		t.Fatalf("snapshot leaked\n%s", snap)
	}
	if strings.Contains(logs.String(), "SYNOPSIS_SENTINEL") || strings.Contains(logs.String(), ".m3u8") {
		t.Fatalf("logs leaked %s", logs.String())
	}
	items, _, err := st.ListJobItems(ctx, job.ID, 1, 50)
	if err != nil {
		t.Fatal(err)
	}
	var minorItem, absentItem *store.SyncJobItem
	for i := range items {
		if items[i].ExternalID == "13" {
			minorItem = &items[i]
		}
		if items[i].ExternalID == "99" {
			absentItem = &items[i]
		}
	}
	if minorItem == nil || minorItem.Action != store.ActionSkipped || minorItem.ItemTitle != "边界样本17岁" || minorItem.PlayURL != "" {
		t.Fatalf("minor item %+v", minorItem)
	}
	if strings.Contains(minorItem.Reason, "SYNOPSIS") || !strings.Contains(minorItem.Reason, "不保存简介") {
		t.Fatalf("reason %s", minorItem.Reason)
	}
	if absentItem == nil || absentItem.Action != store.ActionAbsent || absentItem.ItemTitle != "已下架样本" {
		t.Fatalf("absent item %+v", absentItem)
	}
	for _, hit := range hits {
		if strings.Contains(hit, "/api") || strings.Contains(hit, "m3u8") || strings.Contains(hit, "embed") {
			t.Fatalf("requested forbidden path %s", hit)
		}
	}

	likes = 250
	job2, err := svc.Start(ctx, p.ID, store.ModeFull, store.TriggerManual, true)
	if err != nil || job2.Status != store.StatusSucceeded {
		t.Fatalf("second %+v %v", job2, err)
	}
	again, _, _ := st.ListWorks(ctx, store.WorkFilter{Page: 1, PageSize: 20})
	var got12 int
	for _, w := range again {
		if w.ExternalID == "12" {
			got12++
			if w.LikeCount != 250 || w.AbsentCount != 0 {
				t.Fatalf("snapshot metrics %+v", w)
			}
		}
		if w.ExternalID == "99" && w.AbsentCount != 2 {
			t.Fatalf("second absent %d", w.AbsentCount)
		}
	}
	if got12 != 1 {
		t.Fatalf("work 12 rows %d", got12)
	}

	if err := st.TouchPlatformSync(ctx, p.ID, time.Date(2026, 11, 16, 0, 0, 0, 0, time.UTC)); err != nil {
		t.Fatal(err)
	}
	before := byAbsent(t, st, "99")
	fourteen := byAbsent(t, st, "14")
	inc, err := svc.Start(ctx, p.ID, store.ModeIncremental, store.TriggerSchedule, true)
	if err != nil || inc.Status != store.StatusSucceeded {
		t.Fatalf("incremental %+v %v", inc, err)
	}
	if inc.DiscoveredCount != 2 {
		t.Fatalf("incremental discovered %d", inc.DiscoveredCount)
	}
	if byAbsent(t, st, "99") != before || byAbsent(t, st, "14") != fourteen {
		t.Fatal("incremental changed absent counts")
	}

	if _, err := st.CreateSkipRule(ctx, store.SkipRule{
		Name: "标题屏蔽", RuleType: store.RuleKeyword, Target: store.TargetTitle,
		Pattern: "虚构作品甲", Reason: "标题命中自定义规则", Enabled: true, Priority: 20,
	}); err != nil {
		t.Fatal(err)
	}
	skipped, err := svc.Start(ctx, p.ID, store.ModeFull, store.TriggerManual, true)
	if err != nil || skipped.Status != store.StatusSucceeded {
		t.Fatalf("keyword %+v %v", skipped, err)
	}
	final, _, _ := st.ListWorks(ctx, store.WorkFilter{Page: 1, PageSize: 20})
	for _, w := range final {
		if w.ExternalID == "12" && w.Description != "" {
			t.Fatalf("description kept after skip: %q", w.Description)
		}
	}
	if strings.Contains(st.TextSnapshot(), "这是虚构的测试简介") {
		t.Fatal("synopsis remained after keyword skip")
	}
}

func TestOverlapSkipped(t *testing.T) {
	release := make(chan struct{})
	started := make(chan struct{})
	var once sync.Once
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/sitemap.xml":
			fmt.Fprintf(w, `<urlset xmlns="http://www.sitemaps.org/schemas/sitemap/0.9"><url><loc>http://%s/video/12/</loc><lastmod>2026-11-17</lastmod></url></urlset>`, r.Host)
		case "/video/12/":
			once.Do(func() { close(started) })
			<-release
			http.ServeFile(w, r, "../adapter/huangguoai/testdata/detail_ok.html")
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()
	st := memstore.New()
	ctx := context.Background()
	p, err := st.CreatePlatform(ctx, store.Platform{Code: "huangguoai", Name: "重叠", BaseURL: srv.URL, Enabled: true, CronExpr: timeutil.DefaultCron, SitemapPath: "/sitemap.xml"})
	if err != nil {
		t.Fatal(err)
	}
	svc := New(st, 0)
	svc.Log = discardLog{}
	done := make(chan store.SyncJob, 1)
	go func() {
		job, err := svc.Start(ctx, p.ID, store.ModeFull, store.TriggerManual, true)
		if err != nil {
			t.Errorf("first start: %v", err)
		}
		done <- job
	}()
	select {
	case <-started:
	case <-time.After(5 * time.Second):
		t.Fatal("sync did not reach detail fetch")
	}
	second, err := svc.Start(ctx, p.ID, store.ModeIncremental, store.TriggerManual, true)
	if err != nil {
		t.Fatal(err)
	}
	if second.Status != store.StatusSkipped || second.SkipReason != store.OverlapReason {
		t.Fatalf("overlap %+v", second)
	}
	close(release)
	var first store.SyncJob
	select {
	case first = <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("first sync hung")
	}
	if first.Status != store.StatusSucceeded {
		t.Fatalf("first finished %+v", first)
	}
	has, err := st.HasRunningJob(ctx, p.ID)
	if err != nil || has {
		t.Fatalf("still running %v %v", has, err)
	}
}

func byAbsent(t *testing.T, st *memstore.Store, externalID string) int {
	t.Helper()
	works, _, err := st.ListWorks(context.Background(), store.WorkFilter{Page: 1, PageSize: 50})
	if err != nil {
		t.Fatal(err)
	}
	for _, w := range works {
		if w.ExternalID == externalID {
			return w.AbsentCount
		}
	}
	t.Fatalf("missing %s", externalID)
	return 0
}

type bufLog struct {
	mu sync.Mutex
	b  strings.Builder
}

func (l *bufLog) Printf(format string, args ...any) {
	l.mu.Lock()
	defer l.mu.Unlock()
	fmt.Fprintf(&l.b, format, args...)
	l.b.WriteByte('\n')
}

func (l *bufLog) String() string {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.b.String()
}

type discardLog struct{}

func (discardLog) Printf(string, ...any) {}
