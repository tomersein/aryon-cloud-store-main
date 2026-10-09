// Package handler exposes hierarchies over HTTP.
package handler

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
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
				c.JSON(http.StatusRequestEntityTooLarge, gin.H{"error": fmt.Sprintf("request body is larger than %d MB", MaxBodyBytes>>20)})
				return
			}
			c.JSON(http.StatusBadRequest, gin.H{"error": describeDecodeError(err)})
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
			InternalError(c, err)
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
			InternalError(c, err)
		default:
			c.JSON(http.StatusOK, root)
		}
	}
}

// InternalError logs the cause and keeps database details away from the client.
func InternalError(c *gin.Context, err error) {
	log.Printf("%s %s: %v", c.Request.Method, c.Request.URL.Path, err)
	c.JSON(http.StatusInternalServerError, gin.H{"error": "internal server error"})
}

// describeDecodeError reports what is wrong with the body without exposing Go type names.
func describeDecodeError(err error) string {
	var syntaxErr *json.SyntaxError
	var typeErr *json.UnmarshalTypeError
	switch {
	case errors.As(err, &syntaxErr):
		return fmt.Sprintf("invalid JSON at byte %d", syntaxErr.Offset)
	case errors.As(err, &typeErr) && typeErr.Field != "":
		return fmt.Sprintf("field %q has the wrong type: got a JSON %s", typeErr.Field, typeErr.Value)
	default:
		return "invalid JSON: the body must be a single hierarchy object"
	}
}
