// Package handler exposes hierarchies over HTTP.
package handler

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"

	"go-server/internal/hierarchy"
)

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
		if err := json.NewDecoder(c.Request.Body).Decode(&root); err != nil {
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
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
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
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		default:
			c.JSON(http.StatusOK, root)
		}
	}
}
