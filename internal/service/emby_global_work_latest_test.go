package service

import (
	"reflect"
	"testing"
	"time"
)

func TestGlobalLatestWorksAcrossSourcesAndPlayback(t *testing.T) {
	for _, mixed := range []bool{false, true} {
		t.Run(map[bool]string{false: "ordinary-only", true: "mixed"}[mixed], func(t *testing.T) {
			e := nfoBrowseFixture(t, 0, 0)
			db := e.repo.DB
			e.visibilityCache = map[string]embyVisibilityCacheEntry{e.repo.ReadCacheKey() + "viewer": {visibility: MediaVisibility{AllowedLibraryIDs: []string{"library-nfo"}, HiddenLibraryIDs: []string{"hidden"}, IncludeNSFW: true}, expiresAt: time.Now().Add(time.Hour)}}
			queries := []string{
				`INSERT INTO metadata_items(id,kind,title,source) VALUES ('show','series','Show','local'),('movie','movie','Movie','local'),('empty','series','Empty','local')`,
				`INSERT INTO metadata_items(id,kind,title,source,parent_id,season_num) VALUES ('s1','season','S1','local','show',1),('s2','season','S2','local','show',2)`,
				`INSERT INTO metadata_items(id,kind,title,source,parent_id,episode_num) VALUES ('e1','episode','E1','local','s1',1),('e2','episode','E2','local','s2',1),('e3','episode','Hidden','local','s2',2)`,
				`INSERT INTO media(id,metadata_id,library_id,path,created_at) VALUES ('f1','e1','library-nfo','/f1','2026-01-01'),('f1-alt','e1','library-nfo','/f1-alt','2026-01-01'),('f2','e2','library-nfo','/f2','2026-01-01'),('fh','e3','hidden','/fh','2026-01-04'),('fm','movie','library-nfo','/fm','2026-01-02'),('direct','show','library-nfo','/direct','2026-01-03'),('empty-file','empty','library-nfo','/empty','2026-01-05')`,
				`INSERT INTO playback_histories(id,user_id,metadata_id,media_id,completed) VALUES ('p1','viewer','e1','f1',true)`,
			}
			if mixed {
				queries = append(queries,
					`INSERT INTO hongguo_works(id,source_id,kind,title,related_album_id,season_index,refreshed_at) VALUES ('w1','1','series','HG','album',1,now()),('w2','2','series','HG S2','album',2,now())`,
					`INSERT INTO hongguo_episodes(id,work_id,number) VALUES ('h1','w1',1),('h2','w2',1)`,
					`INSERT INTO nfo_items(id,library_id,local_key,kind,title) VALUES ('nshow','library-nfo','nshow','series','NFO')`,
					`INSERT INTO nfo_items(id,library_id,local_key,kind,title,parent_id,season_num) VALUES ('ns','library-nfo','ns','season','NFO S1','nshow',1)`,
					`INSERT INTO nfo_items(id,library_id,local_key,kind,title,parent_id,episode_num) VALUES ('ne','library-nfo','ne','episode','NFO E1','ns',1)`,
					`INSERT INTO media(id,library_id,catalog_source,path,created_at) VALUES ('hg1','library-nfo','hongguo','/hg1','2026-01-04'),('hg2','library-nfo','hongguo','/hg2','2026-01-01'),('nf','library-nfo','nfo','/nf','2026-01-03')`,
					`INSERT INTO hongguo_media_bindings(media_id,work_id,episode_id) VALUES ('hg1','w1','h1'),('hg2','w2','h2')`,
					`INSERT INTO nfo_media_bindings(media_id,item_id,title,fingerprint) VALUES ('nf','ne','NFO E1','fixture')`,
					`INSERT INTO hongguo_user_states(user_id,source_id,episode_number,completed) VALUES ('viewer','1',1,true)`,
				)
			}
			for _, q := range queries {
				if err := db.Exec(q).Error; err != nil {
					t.Fatal(err)
				}
			}
			check := func(played bool, want []string) {
				t.Helper()
				items, err := e.LatestItems(t.Context(), "viewer", "", 20, played, "BasicSyncInfo")
				if err != nil {
					t.Fatal(err)
				}
				got := []string{}
				seen := map[string]bool{}
				for _, item := range items {
					id := item["Id"].(string)
					if seen[id] || item["Type"] != "Movie" && item["Type"] != "Series" {
						t.Fatalf("invalid work card %s type=%v", id, item["Type"])
					}
					if item["UserData"].(map[string]any)["Played"] != played {
						t.Fatalf("work card %s playback state differs: %v", id, item["UserData"])
					}
					seen[id] = true
					got = append(got, id)
				}
				if !reflect.DeepEqual(got, want) {
					t.Fatalf("played=%v ids=%v want=%v", played, got, want)
				}
			}
			want := []string{"show", "movie"}
			if mixed {
				want = []string{"show", "hg-group-album", "nfo-nshow", "movie"}
			}
			check(false, want)
			check(true, []string{})
			if err := db.Exec(`INSERT INTO playback_histories(id,user_id,metadata_id,media_id,completed) VALUES ('p2','viewer','e2','f2',true)`).Error; err != nil {
				t.Fatal(err)
			}
			check(true, []string{"show"})
			if mixed {
				check(false, []string{"hg-group-album", "nfo-nshow", "movie"})
			} else {
				check(false, []string{"movie"})
			}
		})
	}
}
