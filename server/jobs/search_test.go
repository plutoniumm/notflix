package jobs

import (
	"strings"
	"testing"
)

func TestMergeDedupeKeepsHigherSeeders(t *testing.T) {
	a := []Torrent{{Name: "x", InfoHash: "ABCD", Seeders: 5, Source: "tpb", Magnet: "m1"}}
	b := []Torrent{{Name: "x", InfoHash: "abcd", Seeders: 50, Source: "nyaa", Magnet: "m2"}}
	out := mergeTorrents(a, b)
	if len(out) != 1 {
		t.Fatalf("want 1 result, got %d", len(out))
	}
	if out[0].Seeders != 50 || out[0].Source != "nyaa" {
		t.Fatalf("winner not kept: %+v", out[0])
	}

	out = mergeTorrents(b, a)
	if len(out) != 1 || out[0].Seeders != 50 || out[0].Source != "nyaa" {
		t.Fatalf("order-independence broken: %+v", out)
	}
}

func TestMergeEmptyInfoHash(t *testing.T) {
	out := mergeTorrents([]Torrent{
		{Name: "has magnet", Magnet: "magnet:?xt=...", Seeders: 3},
		{Name: "no magnet", Seeders: 9},
	})
	if len(out) != 1 || out[0].Name != "has magnet" {
		t.Fatalf("empty-hash handling wrong: %+v", out)
	}
}

func TestMergeSortSeedersDesc(t *testing.T) {
	out := mergeTorrents([]Torrent{
		{InfoHash: "a", Magnet: "m", Seeders: 1},
		{InfoHash: "b", Magnet: "m", Seeders: 100},
		{InfoHash: "c", Magnet: "m", Seeders: 10},
	})
	if len(out) != 3 || out[0].Seeders != 100 || out[1].Seeders != 10 || out[2].Seeders != 1 {
		t.Fatalf("not sorted seeders desc: %+v", out)
	}
}

const nyaaXML = `<rss xmlns:atom="http://www.w3.org/2005/Atom" xmlns:nyaa="https://nyaa.si/xmlns/nyaa" version="2.0">
<channel>
<title>Nyaa - "frieren" - Torrent File RSS</title>
<item>
<title>[anime4life.] Frieren S1 BD 1080p Dual Audio</title>
<link>https://nyaa.si/download/2126855.torrent</link>
<pubDate>Tue, 30 Jun 2026 16:23:25 -0000</pubDate>
<nyaa:seeders>109</nyaa:seeders>
<nyaa:leechers>5</nyaa:leechers>
<nyaa:infoHash>27a54cbc8334f7f7c90d43482fb9ef1547bce5a7</nyaa:infoHash>
<nyaa:category>Anime - English-translated</nyaa:category>
<nyaa:size>38.0 GiB</nyaa:size>
<nyaa:trusted>No</nyaa:trusted>
</item>
</channel>
</rss>`

func TestParseNyaa(t *testing.T) {
	out, err := parseNyaa(strings.NewReader(nyaaXML))
	if err != nil {
		t.Fatal(err)
	}
	if len(out) != 1 {
		t.Fatalf("want 1 item, got %d", len(out))
	}
	it := out[0]
	if it.InfoHash != "27a54cbc8334f7f7c90d43482fb9ef1547bce5a7" ||
		it.Seeders != 109 || it.Leechers != 5 || it.Source != "nyaa" ||
		it.Category != "Anime - English-translated" {
		t.Fatalf("bad parse: %+v", it)
	}
	if it.Size != int64(38.0*(1<<30)) {
		t.Fatalf("size wrong: %d", it.Size)
	}
	if it.Added == 0 {
		t.Fatal("pubDate not parsed")
	}
	if !strings.Contains(it.Magnet, "urn:btih:27a54cbc") {
		t.Fatalf("magnet not built: %s", it.Magnet)
	}
}

const knabenJSON = `{"hits":[
{"title":"Frieren S2 - 10","hash":"AABBCC","bytes":1477837572,"seeders":1042,"peers":3,
"date":"2026-05-10T08:26:00+00:00","category":"Anime","magnetUrl":"magnet:?xt=urn:btih:AABBCC&dn=x"},
{"title":"null magnet + no hash","hash":"","bytes":1,"seeders":2,"peers":0,"date":"","category":"","magnetUrl":null}
]}`

func TestParseKnaben(t *testing.T) {
	out, err := parseKnaben(strings.NewReader(knabenJSON))
	if err != nil {
		t.Fatal(err)
	}
	if len(out) != 1 {
		t.Fatalf("want 1 item (hashless+magnetless dropped), got %d", len(out))
	}
	it := out[0]
	if it.InfoHash != "AABBCC" || it.Seeders != 1042 || it.Leechers != 3 ||
		it.Source != "knaben" || it.Magnet != "magnet:?xt=urn:btih:AABBCC&dn=x" {
		t.Fatalf("bad parse: %+v", it)
	}
	if it.Added == 0 {
		t.Fatal("date not parsed")
	}
}

const tcsvJSON = `{"torrents":[
{"infohash":"e738213eba3cb50034e8c37955d7f58c7b9ae53f","name":"[Erai-raws] Frieren - 09.mkv",
"size_bytes":1477837572,"created_unix":1784214068,"seeders":1118,"leechers":37,"completed":950,"id":974}
],"next":975}`

func TestParseTCSV(t *testing.T) {
	out, err := parseTCSV(strings.NewReader(tcsvJSON))
	if err != nil {
		t.Fatal(err)
	}
	if len(out) != 1 {
		t.Fatalf("want 1 item, got %d", len(out))
	}
	it := out[0]
	if it.InfoHash != "e738213eba3cb50034e8c37955d7f58c7b9ae53f" ||
		it.Seeders != 1118 || it.Leechers != 37 || it.Size != 1477837572 ||
		it.Added != 1784214068 || it.Source != "tcsv" {
		t.Fatalf("bad parse: %+v", it)
	}
	if !strings.Contains(it.Magnet, "urn:btih:e738213e") {
		t.Fatalf("magnet not built: %s", it.Magnet)
	}
}

const tpbJSON = `[{"id":"1","name":"Inception 2010","info_hash":"DEADBEEF","leechers":"51","seeders":"539",
"num_files":"2","size":"1991613584","added":"1339547627","category":"207","status":"vip"}]`

func TestParseTPB(t *testing.T) {
	out, err := parseTPB(strings.NewReader(tpbJSON))
	if err != nil {
		t.Fatal(err)
	}
	if len(out) != 1 {
		t.Fatalf("want 1 item, got %d", len(out))
	}
	it := out[0]
	if it.InfoHash != "DEADBEEF" || it.Seeders != 539 || it.Category != "HD Movies" ||
		it.Status != "vip" || it.Source != "tpb" {
		t.Fatalf("bad parse: %+v", it)
	}
}

func TestParseTPBSentinel(t *testing.T) {
	out, err := parseTPB(strings.NewReader(`[{"id":"0","name":"No results returned","info_hash":"0000000000000000000000000000000000000000"}]`))
	if err != nil {
		t.Fatal(err)
	}
	if len(out) != 0 {
		t.Fatalf("sentinel not dropped: %+v", out)
	}
}

func TestLiveProviders(t *testing.T) {
	if testing.Short() {
		t.Skip("live network test")
	}
	provs := map[string]func(string) ([]Torrent, error){
		"tpb":    func(q string) ([]Torrent, error) { return tpbSearch(q, "0") },
		"knaben": knabenSearch,
		"tcsv":   tcsvSearch,
		"nyaa":   nyaaSearch,
	}
	lists := [][]Torrent{}
	for name, fn := range provs {
		ts, err := fn("frieren")
		if err != nil {
			t.Logf("%s: %v", name, err)
			continue
		}
		t.Logf("%s → %d results", name, len(ts))
		if len(ts) == 0 {
			t.Errorf("%s returned 0 results for a popular query", name)
		}
		lists = append(lists, ts)
	}
	if len(lists) == 0 {
		t.Fatal("all providers failed")
	}
	merged := mergeTorrents(lists...)
	t.Logf("merged → %d", len(merged))
	for i := 1; i < len(merged); i++ {
		if merged[i].Seeders > merged[i-1].Seeders {
			t.Fatal("merged result not sorted seeders desc")
		}
	}
}
