package http

import (
	"net/http"

	"github.com/IQVO/product-content/internal/application/usecases"
)

func (s *Server) handleCreateCategory(w http.ResponseWriter, r *http.Request) {
	var req categoryRequest
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, r, err)
		return
	}
	view, err := s.CreateCategory.Execute(r.Context(), usecases.CreateCategoryInput{
		ParentID: parentOrRoot(req.ParentID), Name: req.Name,
	})
	if err != nil {
		writeError(w, r, err)
		return
	}
	writeJSON(w, http.StatusCreated, categoryDTO(view))
}

func (s *Server) handleListCategories(w http.ResponseWriter, r *http.Request) {
	view, err := s.ListCategories.Execute(r.Context())
	if err != nil {
		writeError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, categoryListDTO(view))
}

func (s *Server) handlePublishCategories(w http.ResponseWriter, r *http.Request) {
	view, err := s.PublishCategoryTree.Execute(r.Context())
	if err != nil {
		writeError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, categoryListDTO(view))
}

func (s *Server) handleMoveCategory(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r, "categoryId")
	if !ok {
		return
	}
	var req moveRequest
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, r, err)
		return
	}
	view, err := s.MoveCategory.Execute(r.Context(), usecases.MoveCategoryInput{
		CategoryID: id, ParentID: parentOrRoot(req.ParentID),
	})
	if err != nil {
		writeError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, categoryDTO(view))
}

type renameRequest struct {
	Name string `json:"name"`
}

func (s *Server) handleRenameCategory(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r, "categoryId")
	if !ok {
		return
	}
	var req renameRequest
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, r, err)
		return
	}
	view, err := s.RenameCategory.Execute(r.Context(), usecases.RenameCategoryInput{CategoryID: id, Name: req.Name})
	if err != nil {
		writeError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, categoryDTO(view))
}
