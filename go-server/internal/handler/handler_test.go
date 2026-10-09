package handler

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"

	"go-server/internal/hierarchy"
)

type fakeStore struct {
	saved   *hierarchy.FlatRows
	saveErr error
	tree    *hierarchy.Node
	loadErr error
}

func (f *fakeStore) Save(_ context.Context, rows *hierarchy.FlatRows) error {
	f.saved = rows
	return f.saveErr
}

func (f *fakeStore) Load(_ context.Context, _ int64) (*hierarchy.Node, error) {
	return f.tree, f.loadErr
}

func serve(store Store, method, path, body string) *httptest.ResponseRecorder {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	Register(r, store)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, httptest.NewRequest(method, path, strings.NewReader(body)))
	return rec
}

const internalErrorBody = `{"error":"internal server error"}`

const validTree = `{"id":1,"type":"management_group","children":[{"id":2,"type":"subscription","children":[]}]}`

func TestPostHierarchy(t *testing.T) {
	tests := map[string]struct {
		body      string
		saveErr   error
		wantCode  int
		wantSaved bool
		wantBody  string
	}{
		"valid tree":        {validTree, nil, http.StatusOK, true, ""},
		"broken JSON":       {`{"id":`, nil, http.StatusBadRequest, false, ""},
		"invalid tree":      {`{"id":1,"type":"vm","children":[]}`, nil, http.StatusBadRequest, false, ""},
		"body over limit":   {strings.Repeat(" ", MaxBodyBytes) + validTree, nil, http.StatusRequestEntityTooLarge, false, ""},
		"would form a loop": {validTree, hierarchy.ErrCycle, http.StatusConflict, true, ""},
		"database failure":  {validTree, errors.New(`pq: relation "nodes" does not exist`), http.StatusInternalServerError, true, internalErrorBody},
	}
	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			store := &fakeStore{saveErr: tc.saveErr}
			rec := serve(store, http.MethodPost, "/hierarchy", tc.body)
			if rec.Code != tc.wantCode {
				t.Fatalf("status = %d, want %d (body %s)", rec.Code, tc.wantCode, rec.Body)
			}
			if (store.saved != nil) != tc.wantSaved {
				t.Fatalf("store called = %v, want %v", store.saved != nil, tc.wantSaved)
			}
			if tc.wantBody != "" && rec.Body.String() != tc.wantBody {
				t.Fatalf("body = %s, want %s", rec.Body, tc.wantBody)
			}
		})
	}
}

func TestGetHierarchy(t *testing.T) {
	tree := &hierarchy.Node{ID: 1, Type: "management_group", Children: []*hierarchy.Node{}}
	tests := map[string]struct {
		path     string
		store    *fakeStore
		wantCode int
		wantBody string
	}{
		"found":          {"/hierarchy/1", &fakeStore{tree: tree}, http.StatusOK, `{"id":1,"type":"management_group","children":[]}`},
		"not found":      {"/hierarchy/7", &fakeStore{loadErr: hierarchy.ErrNotFound}, http.StatusNotFound, ""},
		"non-numeric id": {"/hierarchy/abc", &fakeStore{}, http.StatusBadRequest, ""},
		"database error": {"/hierarchy/1", &fakeStore{loadErr: errors.New("pq: canceling statement")}, http.StatusInternalServerError, internalErrorBody},
	}
	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			rec := serve(tc.store, http.MethodGet, tc.path, "")
			if rec.Code != tc.wantCode {
				t.Fatalf("status = %d, want %d", rec.Code, tc.wantCode)
			}
			if tc.wantBody != "" && rec.Body.String() != tc.wantBody {
				t.Fatalf("body = %s, want %s", rec.Body, tc.wantBody)
			}
		})
	}
}
