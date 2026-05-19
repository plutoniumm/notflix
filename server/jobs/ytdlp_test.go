package jobs

import "testing"

func TestIsYTCandidate(t *testing.T) {
	cases := []struct {
		uri  string
		want bool
	}{
		{"https://youtube.com/watch?v=abc", true},
		{"https://youtu.be/abc", true},
		{"https://vimeo.com/123456", true},
		{"http://example.com/page", true},
		{"https://example.com/video.mp4", false},
		{"https://example.com/video.mp4?token=xyz", false},
		{"https://example.com/file.MKV", false},
		{"https://cdn.example.com/x.torrent", false},
		{"magnet:?xt=urn:btih:abc", false},
		{"ftp://example.com/x", false},
		{"   https://youtube.com/watch?v=abc   ", true},
	}
	for _, c := range cases {
		got := IsYTCandidate(c.uri)
		if got != c.want {
			t.Errorf("IsYTCandidate(%q) = %v, want %v", c.uri, got, c.want)
		}
	}
}

func TestIsYT(t *testing.T) {
	if !IsYT("yt0123abcd") {
		t.Error("yt-prefixed gid should be yt-dlp")
	}
	if IsYT("abcd1234") {
		t.Error("aria2 gid should not be yt-dlp")
	}
}

func TestParseYTLine_Progress(t *testing.T) {
	j := &ytJob{}
	parseYTLine(j, "[download]  12.3% of  100.00MiB at  5.00MiB/s ETA 00:18")
	if j.Pct != 12.3 {
		t.Errorf("Pct = %v, want 12.3", j.Pct)
	}
	if j.Total != 100*(1<<20) {
		t.Errorf("Total = %d, want %d", j.Total, 100*(1<<20))
	}
	if j.Speed != 5*(1<<20) {
		t.Errorf("Speed = %d, want %d", j.Speed, 5*(1<<20))
	}
}

func TestParseYTLine_Destination(t *testing.T) {
	j := &ytJob{}
	parseYTLine(j, `[download] Destination: /Volumes/Ravan/Cool_Video.mp4`)
	if j.Title != "Cool_Video.mp4" {
		t.Errorf("Title = %q, want Cool_Video.mp4", j.Title)
	}
}

func TestParseYTLine_MergeDestination(t *testing.T) {
	j := &ytJob{}
	parseYTLine(j, `[Merger] Merging formats into "/Volumes/Ravan/Some Title.mp4"`)
	if j.Title != "Some Title.mp4" {
		t.Errorf("Title = %q, want 'Some Title.mp4'", j.Title)
	}
}

func TestParseYTLine_PartialAtStart(t *testing.T) {
	j := &ytJob{}
	parseYTLine(j, "[download]   0.0% of ~  10.50GiB at Unknown B/s ETA Unknown")
	if j.Pct != 0 {
		t.Errorf("Pct = %v, want 0", j.Pct)
	}
	if j.Total != int64(10.5*float64(1<<30)) {
		t.Errorf("Total = %d, want %d", j.Total, int64(10.5*float64(1<<30)))
	}
}
