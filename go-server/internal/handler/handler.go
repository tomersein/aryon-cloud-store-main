// Package handler exposes hierarchies over HTTP.
package handler

import (
	"context"
	"encoding/json"
	"errors"
	"log"
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"

	"go-server/internal/hierarchy"
)

// MaxBodyBytes caps a POST body; about 500,000 nodes fit in it.
const MaxBodyBytes = 32 << 20

type Store interface {
	Save(ctx context.Context, rows *hierarchy.FlatRows) error
	Load(ctx context.Context, rootID int64) (*hierarchy.Node, error)
}

func Register(r *gin.Engine, store Store) {
	r.POST("/hierarchy", postHierarchy(store))
	r.GET("/hierarchy/:id", getHierarchy(store))
}

func postHierarchy(store Store) gin.HandlerFunc {
	return func(c *gin.Context) {
		var root hierarchy.NodeInput
		body := http.MaxBytesReader(c.Writer, c.Request.Body, MaxBodyBytes)
		if err := json.NewDecoder(body).Decode(&root); err != nil {
			var tooLarge *http.MaxBytesError
			if errors.As(err, &tooLarge) {
				c.JSON(http.StatusRequestEntityTooLarge, gin.H{"error": "request body is larger than 32 MB"})
				return
			}
			c.JSON(http.StatusBadRequest, gin.H{"error": "invalid JSON: " + err.Error()})
			return
		}
		rows, err := hierarchy.Flatten(&root)
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}

		err = store.Save(c.Request.Context(), rows)
		switch {
		case errors.Is(err, hierarchy.ErrCycle):
			c.JSON(http.StatusConflict, gin.H{"error": err.Error()})
		case err != nil:
			internalError(c, err)
		default:
			c.JSON(http.StatusOK, gin.H{"id": *root.ID, "nodes": len(rows.IDs)})
		}
	}
}

func getHierarchy(store Store) gin.HandlerFunc {
	return func(c *gin.Context) {
		id, err := strconv.ParseInt(c.Param("id"), 10, 64)
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "node id must be an integer"})
			return
		}

		root, err := store.Load(c.Request.Context(), id)
		switch {
		case errors.Is(err, hierarchy.ErrNotFound):
			c.JSON(http.StatusNotFound, gin.H{"error": err.Error()})
		case err != nil:
			internalError(c, err)
		default:
			c.JSON(http.StatusOK, root)
		}
	}
}

// internalError logs the cause and keeps database details away from the client.
func internalError(c *gin.Context, err error) {
	log.Printf("%s %s: %v", c.Request.Method, c.Request.URL.Path, err)
	c.JSON(http.StatusInternalServerError, gin.H{"error": "internal server error"})
}
