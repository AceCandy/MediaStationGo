// Package handler — server-side file browser used by the React
// "select library path" dialog and the Storage tab.
package handler

import (
	"errors"
	"net/http"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"

	"github.com/ShukeBta/MediaStationGo/internal/service"
)

type fileFolderReq struct {
	Path string `json:"path"`
	Name string `json:"name"`
}

type fileRenameReq struct {
	Path string `json:"path"`
	Name string `json:"name"`
}

type fileTransferReq struct {
	SourcePath   string `json:"source_path"`
	DestPath     string `json:"dest_path"`
	TransferMode string `json:"transfer_mode"`
}

type strmDeleteReq struct {
	DeleteParent bool `json:"delete_parent"`
}

func browseFilesHandler(svc *service.Container) gin.HandlerFunc {
	return func(c *gin.Context) {
		path := c.Query("path")
		max, _ := strconv.Atoi(c.DefaultQuery("max", "1000"))
		recursive := strings.EqualFold(c.DefaultQuery("recursive", "false"), "true")
		listing, err := svc.FileManager.List(path, max, recursive)
		writeFileManagerResponse(c, listing, err)
	}
}

func createFolderHandler(svc *service.Container) gin.HandlerFunc {
	return func(c *gin.Context) {
		var req fileFolderReq
		if err := c.ShouldBindJSON(&req); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}
		res, err := svc.FileManager.CreateFolder(req.Path, req.Name)
		writeFileManagerResponse(c, res, err)
	}
}

func renameFileHandler(svc *service.Container) gin.HandlerFunc {
	return func(c *gin.Context) {
		var req fileRenameReq
		if err := c.ShouldBindJSON(&req); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}
		res, err := svc.FileManager.Rename(req.Path, req.Name)
		writeFileManagerResponse(c, res, err)
	}
}

func deleteFileHandler(svc *service.Container) gin.HandlerFunc {
	return func(c *gin.Context) {
		path := c.Query("path")
		if strings.TrimSpace(path) == "" {
			c.JSON(http.StatusBadRequest, gin.H{"error": "path required"})
			return
		}
		err := svc.FileManager.Delete(path)
		writeFileManagerResponse(c, gin.H{"removed": true}, err)
	}
}

func getSTRMDeleteTargetHandler(svc *service.Container) gin.HandlerFunc {
	return func(c *gin.Context) {
		result, err := svc.FileManager.ResolveSTRMDeleteTarget(c.Request.Context(), c.Param("id"))
		writeSTRMDeleteResponse(c, result, err)
	}
}

func deleteSTRMTargetHandler(svc *service.Container) gin.HandlerFunc {
	return func(c *gin.Context) {
		var req strmDeleteReq
		if err := c.ShouldBindJSON(&req); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}
		path, err := svc.FileManager.DeleteSTRMTarget(c.Request.Context(), c.Param("id"), req.DeleteParent)
		writeSTRMDeleteResponse(c, gin.H{"removed": true, "path": path}, err)
	}
}

func transferFileHandler(svc *service.Container) gin.HandlerFunc {
	return func(c *gin.Context) {
		var req fileTransferReq
		if err := c.ShouldBindJSON(&req); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}
		res, err := svc.FileManager.Transfer(req.SourcePath, req.DestPath, service.TransferMode(req.TransferMode))
		writeFileManagerResponse(c, res, err)
	}
}

func writeFileManagerResponse(c *gin.Context, payload any, err error) {
	if err != nil {
		if errors.Is(err, service.ErrPathOutOfBounds) || errors.Is(err, service.ErrRootMutation) {
			c.JSON(http.StatusForbidden, gin.H{"error": err.Error()})
			return
		}
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, payload)
}

func writeSTRMDeleteResponse(c *gin.Context, payload any, err error) {
	if errors.Is(err, service.ErrMediaNotFound) {
		c.JSON(http.StatusNotFound, gin.H{"error": "not found"})
		return
	}
	writeFileManagerResponse(c, payload, err)
}
