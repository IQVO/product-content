package http

import (
	"net/http"

	"github.com/go-chi/chi/v5"

	"github.com/IQVO/product-content/internal/application/ports"
	"github.com/IQVO/product-content/internal/application/usecases"
	"github.com/IQVO/product-content/internal/domain/content"
)

// maxIDLength bounds a path identifier; a longer one cannot name a resource.
const maxIDLength = 128

// pathID returns the named path parameter, or false after writing 404 for one
// that can never name a resource.
func pathID(w http.ResponseWriter, r *http.Request, name string) (string, bool) {
	id := chi.URLParam(r, name)
	if id == "" || len(id) > maxIDLength {
		writeError(w, r, ports.ErrNotFound)
		return "", false
	}
	return id, true
}

func handleHealthz(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, statusBody{Status: "ok"})
}

func (s *Server) handleCreate(w http.ResponseWriter, r *http.Request) {
	var req contentRequest
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, r, err)
		return
	}
	view, err := s.CreateProductContent.Execute(r.Context(), usecases.ContentInput{
		CatalogItemID: content.ID(req.CatalogItemID), Body: req.body(),
	})
	if err != nil {
		writeError(w, r, err)
		return
	}
	writeJSON(w, http.StatusCreated, contentDTO(view))
}

func (s *Server) handleRevise(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r, "catalogItemId")
	if !ok {
		return
	}
	var req contentRequest
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, r, err)
		return
	}
	cid, body, err := req.forRevise(id)
	if err != nil {
		writeError(w, r, err)
		return
	}
	view, err := s.ReviseContent.Execute(r.Context(), usecases.ContentInput{CatalogItemID: cid, Body: body})
	if err != nil {
		writeError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, contentDTO(view))
}

// handleAction serves the body-less lifecycle routes (POST /publications,
// POST /retirements, DELETE /draft) through run.
func handleAction(run func(r *http.Request, id content.ID) (contentResponse, error)) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id, ok := pathID(w, r, "catalogItemId")
		if !ok {
			return
		}
		out, err := run(r, content.ID(id))
		if err != nil {
			writeError(w, r, err)
			return
		}
		writeJSON(w, http.StatusOK, out)
	}
}

func (s *Server) handlePublish(w http.ResponseWriter, r *http.Request) {
	handleAction(func(r *http.Request, id content.ID) (contentResponse, error) {
		v, err := s.PublishContent.Execute(r.Context(), id)
		return contentDTO(v), err
	})(w, r)
}

func (s *Server) handleRetire(w http.ResponseWriter, r *http.Request) {
	handleAction(func(r *http.Request, id content.ID) (contentResponse, error) {
		v, err := s.RetireContent.Execute(r.Context(), id)
		return contentDTO(v), err
	})(w, r)
}

func (s *Server) handleDiscard(w http.ResponseWriter, r *http.Request) {
	handleAction(func(r *http.Request, id content.ID) (contentResponse, error) {
		v, err := s.DiscardDraft.Execute(r.Context(), id)
		return contentDTO(v), err
	})(w, r)
}

func (s *Server) handleGet(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r, "catalogItemId")
	if !ok {
		return
	}
	view, err := s.GetProductContent.Execute(r.Context(), content.ID(id))
	if err != nil {
		writeError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, contentDTO(view))
}

func (s *Server) handleList(w http.ResponseWriter, r *http.Request) {
	limit, after, err := pageParams(r)
	if err != nil {
		writeError(w, r, err)
		return
	}
	res, err := s.ListProductContents.Execute(r.Context(), usecases.ListProductContentsInput{After: after, Limit: limit})
	if err != nil {
		writeError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, contentPageOut(res))
}
