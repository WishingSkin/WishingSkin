package httpapi

import (
	"encoding/json"
	"net/http/httptest"
	"sync"
	"testing"
	"time"
)

func TestTextureLikesAndTrendingBothDatabases(t *testing.T) {
	for _, dialect := range []string{"mysql", "postgres"} {
		t.Run(dialect, func(t *testing.T) {
			f := newManagementApp(t, dialect)
			oldPopular := uploadedRecord(t, f.upload(1, testPNG(t, 64, 64), map[string]string{"name": "AlphaOld", "isPublic": "true"}))
			freshRising := uploadedRecord(t, f.upload(2, testPNG(t, 64, 64), map[string]string{"name": "BetaFresh", "isPublic": "true"}))
			privateRec := uploadedRecord(t, f.upload(1, testPNG(t, 64, 64), map[string]string{"name": "PrivateSkin"}))

			// Auth and visibility checks.
			webStatus(t, f.call(0, "PUT", "/api/library/"+oldPopular.ID+"/like", nil), 401)
			webStatus(t, f.call(2, "PUT", "/api/library/"+privateRec.ID+"/like", nil), 404)
			webStatus(t, f.call(1, "PUT", "/api/library/"+privateRec.ID+"/like", nil), 200)

			// Concurrent PUT likes must be idempotent.
			var wg sync.WaitGroup
			responses := make(chan *httptest.ResponseRecorder, 6)
			for range 6 {
				wg.Add(1)
				go func() {
					defer wg.Done()
					responses <- f.call(2, "PUT", "/api/library/"+freshRising.ID+"/like", nil)
				}()
			}
			wg.Wait()
			close(responses)
			for w := range responses {
				webStatus(t, w, 200)
			}

			// Give oldPopular 2 historical likes from 45 days ago (outside the 30-day recent boost window).
			webStatus(t, f.call(1, "PUT", "/api/library/"+oldPopular.ID+"/like", nil), 200)
			webStatus(t, f.call(3, "PUT", "/api/library/"+oldPopular.ID+"/like", nil), 200)
			oldStamp := time.Now().Add(-45 * 24 * time.Hour).Unix()
			f.exec("UPDATE texture_likes SET created_at=? WHERE record_id=?", oldStamp, oldPopular.ID)

			// sort=likes puts oldPopular (2 likes) ahead of freshRising (1 like).
			w := f.call(2, "GET", "/api/library?sort=likes", nil)
			webStatus(t, w, 200)
			var likedList struct {
				Textures []libraryRecord `json:"textures"`
			}
			if err := json.Unmarshal(w.Body.Bytes(), &likedList); err != nil || len(likedList.Textures) != 2 {
				t.Fatalf("unexpected likes list: %v %s", err, w.Body.String())
			}
			if likedList.Textures[0].ID != oldPopular.ID || likedList.Textures[0].LikeCount != 2 || likedList.Textures[0].Liked {
				t.Fatalf("unexpected first item for sort=likes: %+v", likedList.Textures[0])
			}
			if likedList.Textures[1].ID != freshRising.ID || likedList.Textures[1].LikeCount != 1 || !likedList.Textures[1].Liked {
				t.Fatalf("unexpected second item for sort=likes: %+v", likedList.Textures[1])
			}

			// sort=trending boosts freshRising (1 recent like = 4+2=6) over oldPopular (2 old likes = 0+4=4).
			w = f.call(0, "GET", "/api/library?sort=trending", nil)
			webStatus(t, w, 200)
			var trendingList struct {
				Textures []libraryRecord `json:"textures"`
			}
			if err := json.Unmarshal(w.Body.Bytes(), &trendingList); err != nil || len(trendingList.Textures) != 2 {
				t.Fatalf("unexpected trending list: %v %s", err, w.Body.String())
			}
			if trendingList.Textures[0].ID != freshRising.ID || trendingList.Textures[0].Liked {
				t.Fatalf("expected freshRising first in trending and guest liked=false, got %+v", trendingList.Textures[0])
			}

			// Unlike decrements cleanly and is idempotent.
			unlike := f.call(2, "DELETE", "/api/library/"+freshRising.ID+"/like", nil)
			webStatus(t, unlike, 200)
			var state struct {
				Liked     bool  `json:"liked"`
				LikeCount int64 `json:"likeCount"`
			}
			if err := json.Unmarshal(unlike.Body.Bytes(), &state); err != nil || state.Liked || state.LikeCount != 0 {
				t.Fatalf("unexpected unlike response: %+v %v", state, err)
			}
			webStatus(t, f.call(2, "DELETE", "/api/library/"+freshRising.ID+"/like", nil), 200)

			// Deleting texture record clears its likes.
			webStatus(t, f.call(1, "DELETE", "/api/textures/"+oldPopular.ID, nil), 204)
			var remaining int
			if err := f.db.DB.QueryRow(f.db.Bind("SELECT COUNT(*) FROM texture_likes WHERE record_id=?"), oldPopular.ID).Scan(&remaining); err != nil || remaining != 0 {
				t.Fatalf("expected likes cleared on texture delete, got count=%d err=%v", remaining, err)
			}

			// Homepage showcase source validation.
			webStatus(t, f.call(3, "PATCH", "/api/admin/homepage", homepageSettings{
				Background: homepageBackground{Overlay: 65},
				Blocks: []homepageBlock{{
					ID: "hot", Kind: "showcase", Enabled: true, Title: "热门皮肤", Source: "trending", ButtonLabel: "去公开库", ButtonHref: "/library",
				}},
			}), 200)
			webStatus(t, f.call(3, "PATCH", "/api/admin/homepage", homepageSettings{
				Background: homepageBackground{Overlay: 65},
				Blocks: []homepageBlock{{
					ID: "bad", Kind: "showcase", Enabled: true, Title: "Bad", Source: "invalid",
				}},
			}), 400)
			webStatus(t, f.call(3, "PATCH", "/api/admin/homepage", homepageSettings{
				Background: homepageBackground{Overlay: 65},
				Blocks: []homepageBlock{{
					ID: "bad2", Kind: "cta", Enabled: true, Title: "Bad", Source: "trending",
				}},
			}), 400)
		})
	}
}
