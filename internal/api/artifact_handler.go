package api

import (
	"bytes"
	"errors"
	"io"
	"net/http"
	"strconv"

	"github.com/sudebaker/acb-go/internal/db"
	"github.com/sudebaker/acb-go/internal/models"
	"github.com/sudebaker/acb-go/internal/rustfs"
	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
)

type ArtifactHandler struct {
	taskRepo *db.TaskRepo
	rustfs   *rustfs.Client
}

func (h *ArtifactHandler) UploadArtifact(w http.ResponseWriter, r *http.Request) {
	taskID := chi.URLParam(r, "id")

	task, err := h.taskRepo.GetByID(taskID)
	if err != nil {
		WriteError(w, 500, "get_failed", err.Error())
		return
	}
	if task == nil {
		WriteError(w, 404, "not_found", "task not found")
		return
	}

	if err := r.ParseMultipartForm(32 << 20); err != nil {
		WriteError(w, 400, "invalid_form", "failed to parse multipart form")
		return
	}

	file, header, err := r.FormFile("file")
	if err != nil {
		WriteError(w, 400, "missing_file", "file field is required")
		return
	}
	defer file.Close()

	if header.Size == 0 {
		WriteError(w, 400, "empty_file", "file is empty")
		return
	}

	headerBuf := make([]byte, 512)
	n, err := io.ReadFull(file, headerBuf)
	if err != nil && err != io.ErrUnexpectedEOF && err != io.EOF {
		WriteError(w, 400, "read_error", "failed to read file")
		return
	}
	headerBuf = headerBuf[:n]

	contentType := http.DetectContentType(headerBuf)
	combined := io.MultiReader(bytes.NewReader(headerBuf), file)

	key := taskID + "/" + uuid.New().String() + "-" + header.Filename
	size := header.Size

	if err := h.rustfs.Upload(r.Context(), key, combined, size, contentType); err != nil {
		WriteError(w, 500, "upload_failed", err.Error())
		return
	}

	artifact := models.Artifact{
		Key:         key,
		Bucket:      h.rustfs.Bucket(),
		Size:        size,
		ContentType: contentType,
	}

	if err := h.taskRepo.AddArtifact(taskID, artifact); err != nil {
		WriteError(w, 500, "add_artifact_failed", err.Error())
		return
	}

	WriteJSON(w, 201, artifact)
}

func (h *ArtifactHandler) DispatchListOrDownload(w http.ResponseWriter, r *http.Request) {
	if r.URL.Query().Has("key") {
		h.DownloadArtifact(w, r)
		return
	}
	h.ListArtifacts(w, r)
}

func (h *ArtifactHandler) ListArtifacts(w http.ResponseWriter, r *http.Request) {
	taskID := chi.URLParam(r, "id")

	task, err := h.taskRepo.GetByID(taskID)
	if err != nil {
		WriteError(w, 500, "get_failed", err.Error())
		return
	}
	if task == nil {
		WriteError(w, 404, "not_found", "task not found")
		return
	}

	artifacts, err := h.taskRepo.GetArtifacts(taskID)
	if err != nil {
		WriteError(w, 500, "list_failed", err.Error())
		return
	}

	if artifacts == nil {
		artifacts = []models.Artifact{}
	}

	WriteJSON(w, 200, artifacts)
}

func (h *ArtifactHandler) DownloadArtifact(w http.ResponseWriter, r *http.Request) {
	taskID := chi.URLParam(r, "id")
	key := r.URL.Query().Get("key")

	if key == "" {
		WriteError(w, 400, "missing_key", "key query parameter is required")
		return
	}

	task, err := h.taskRepo.GetByID(taskID)
	if err != nil {
		WriteError(w, 500, "get_failed", err.Error())
		return
	}
	if task == nil {
		WriteError(w, 404, "not_found", "task not found")
		return
	}

	body, contentType, size, err := h.rustfs.Download(r.Context(), key)
	if err != nil {
		if errors.Is(err, rustfs.ErrNotFound) {
			WriteError(w, 404, "not_found", "artifact not found")
			return
		}
		WriteError(w, 500, "download_failed", err.Error())
		return
	}
	if body == nil {
		WriteError(w, 500, "download_failed", "no data returned")
		return
	}
	defer body.Close()

	w.Header().Set("Content-Type", contentType)
	w.Header().Set("Content-Length", strconv.FormatInt(size, 10))
	w.WriteHeader(200)
	io.Copy(w, body)
}

func (h *ArtifactHandler) DeleteArtifact(w http.ResponseWriter, r *http.Request) {
	taskID := chi.URLParam(r, "id")
	key := r.URL.Query().Get("key")

	if key == "" {
		WriteError(w, 400, "missing_key", "key query parameter is required")
		return
	}

	task, err := h.taskRepo.GetByID(taskID)
	if err != nil {
		WriteError(w, 500, "get_failed", err.Error())
		return
	}
	if task == nil {
		WriteError(w, 404, "not_found", "task not found")
		return
	}

	exists, err := h.rustfs.Exists(r.Context(), key)
	if err != nil {
		WriteError(w, 500, "check_failed", err.Error())
		return
	}
	if !exists {
		WriteError(w, 404, "not_found", "artifact not found")
		return
	}

	if err := h.rustfs.Delete(r.Context(), key); err != nil {
		WriteError(w, 500, "delete_failed", err.Error())
		return
	}

	if err := h.taskRepo.RemoveArtifact(taskID, key); err != nil {
		WriteError(w, 500, "remove_artifact_failed", err.Error())
		return
	}

	w.WriteHeader(204)
}
