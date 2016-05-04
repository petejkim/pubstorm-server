package certs

import (
	"bytes"
	"fmt"
	"io"
	"io/ioutil"
	"net/http"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/jinzhu/gorm"
	"github.com/nitrous-io/rise-server/apiserver/common"
	"github.com/nitrous-io/rise-server/apiserver/controllers"
	"github.com/nitrous-io/rise-server/apiserver/dbconn"
	"github.com/nitrous-io/rise-server/apiserver/models/cert"
	"github.com/nitrous-io/rise-server/apiserver/models/domain"
	"github.com/nitrous-io/rise-server/pkg/aesencrypter"
	"github.com/nitrous-io/rise-server/pkg/certhelper"
	"github.com/nitrous-io/rise-server/pkg/pubsub"
	"github.com/nitrous-io/rise-server/shared"
	"github.com/nitrous-io/rise-server/shared/exchanges"
	"github.com/nitrous-io/rise-server/shared/messages"
	"github.com/nitrous-io/rise-server/shared/s3client"
)

var MaxCertSize = int64(96 * 1024) // 96 kb

func Show(c *gin.Context) {
	proj := controllers.CurrentProject(c)
	domainName := c.Param("name")

	db, err := dbconn.DB()
	if err != nil {
		controllers.InternalServerError(c, err)
		return
	}

	ct := &cert.Cert{}
	if err := db.Table("certs").
		Joins("JOIN domains ON domains.id = certs.domain_id").
		Where("domains.name = ? AND project_id = ?", domainName, proj.ID).First(ct).Error; err != nil {

		if err == gorm.RecordNotFound {
			c.JSON(http.StatusNotFound, gin.H{
				"error":             "not_found",
				"error_description": "cert could not be found",
			})
			return
		}

		controllers.InternalServerError(c, err)
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"cert": ct.AsJSON(),
	})
}

func Create(c *gin.Context) {
	proj := controllers.CurrentProject(c)
	domainName := c.Param("name")

	if strings.HasSuffix(domainName, shared.DefaultDomain) {
		c.JSON(http.StatusForbidden, gin.H{
			"error":             "forbidden",
			"error_description": "not allowed to upload certs for default domain",
		})
		return
	}

	// get the multipart reader for the request.
	reader, err := c.Request.MultipartReader()
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"error":             "invalid_request",
			"error_description": "the request should be encoded in multipart/form-data format",
		})
		return
	}

	if n, err := strconv.ParseInt(c.Request.Header.Get("Content-Length"), 10, 64); err != nil || n > MaxCertSize {
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{
				"error":             "invalid_request",
				"error_description": "Content-Length header is required",
			})
		} else {
			c.JSON(http.StatusBadRequest, gin.H{
				"error":             "invalid_request",
				"error_description": "request body is too large",
			})
		}
		return
	}

	db, err := dbconn.DB()
	if err != nil {
		controllers.InternalServerError(c, err)
		return
	}

	d := &domain.Domain{}
	if err := db.Where("name = ? AND project_id = ?", domainName, proj.ID).First(d).Error; err != nil {
		if err == gorm.RecordNotFound {
			c.JSON(http.StatusNotFound, gin.H{
				"error":             "not_found",
				"error_description": "domain could not be found",
			})
			return
		}

		controllers.InternalServerError(c, err)
		return
	}

	ct := &cert.Cert{
		DomainID: d.ID,
	}

	var certBytes, pKeyBytes []byte
	for {
		part, err := reader.NextPart()
		if err == io.EOF {
			break
		}

		if part.FormName() == "" {
			continue
		}

		switch part.FormName() {
		case "cert":
			certBytes, err = ioutil.ReadAll(part)
			if err != nil {
				controllers.InternalServerError(c, err)
				return
			}
			ct.CertificatePath = fmt.Sprintf("certs/%s/ssl.crt", domainName)
		case "key":
			pKeyBytes, err = ioutil.ReadAll(part)
			if err != nil {
				controllers.InternalServerError(c, err)
				return
			}
			ct.PrivateKeyPath = fmt.Sprintf("certs/%s/ssl.key", domainName)
		default:
			c.JSON(422, gin.H{
				"error":             "invalid_params",
				"error_description": "unrecognized form field",
			})
			return
		}
	}

	if certBytes == nil || pKeyBytes == nil {
		c.JSON(422, gin.H{
			"error":             "invalid_params",
			"error_description": "both cert and key are required",
		})
		return
	}

	info, err := certhelper.GetInfo(certBytes, pKeyBytes, domainName)
	if err != nil {
		if err == certhelper.ErrInvalidCert {
			c.JSON(422, gin.H{
				"error":             "invalid_params",
				"error_description": "invalid cert or key",
			})
		} else if err == certhelper.ErrInvalidCommonName {
			c.JSON(422, gin.H{
				"error":             "invalid_params",
				"error_description": "invalid common name (domain name mismatch)",
			})
		} else {
			controllers.InternalServerError(c, err)
		}
		return
	}

	ct.StartsAt = info.StartsAt
	ct.ExpiresAt = info.ExpiresAt
	ct.CommonName = &info.CommonName
	ct.Issuer = &info.Issuer
	ct.Subject = &info.Subject

	uploadKeys := []string{ct.CertificatePath, ct.PrivateKeyPath}
	for i, b := range [][]byte{certBytes, pKeyBytes} {
		cipherText, err := aesencrypter.Encrypt(b, []byte(common.AesKey))
		if err != nil {
			controllers.InternalServerError(c, err)
			return
		}

		var reader io.Reader
		reader = bytes.NewReader(cipherText)
		if err := s3client.Upload(uploadKeys[i], reader, "", "private"); err != nil {
			controllers.InternalServerError(c, err)
			return
		}
	}

	// Invalidate cert cache
	m, err := pubsub.NewMessageWithJSON(exchanges.Edges, exchanges.RouteV1Invalidation, &messages.V1InvalidationMessageData{
		Domains: []string{domainName},
	})

	if err != nil {
		controllers.InternalServerError(c, err)
		return
	}

	if err := m.Publish(); err != nil {
		controllers.InternalServerError(c, err)
		return
	}

	if err := cert.Upsert(db, ct); err != nil {
		controllers.InternalServerError(c, err)
		return
	}

	c.JSON(http.StatusCreated, gin.H{
		"cert": ct.AsJSON(),
	})
}

func Destroy(c *gin.Context) {
	proj := controllers.CurrentProject(c)
	domainName := c.Param("name")

	db, err := dbconn.DB()
	if err != nil {
		controllers.InternalServerError(c, err)
		return
	}

	var d domain.Domain
	if err := db.Where("name = ? AND project_id = ?", domainName, proj.ID).First(&d).Error; err != nil {
		if err == gorm.RecordNotFound {
			c.JSON(http.StatusNotFound, gin.H{
				"error":             "not_found",
				"error_description": "cert could not be found",
			})
			return
		}
		controllers.InternalServerError(c, err)
		return
	}

	q := db.Where("domain_id = ?", d.ID).Delete(cert.Cert{})
	if q.Error != nil {
		controllers.InternalServerError(c, err)
		return
	}

	if q.RowsAffected == 0 {
		c.JSON(http.StatusNotFound, gin.H{
			"error":             "not_found",
			"error_description": "cert could not be found",
		})
		return
	}

	certificatePath := "certs/" + domainName + "/ssl.crt"
	privateKeyPath := "certs/" + domainName + "/ssl.key"
	if err := s3client.Delete(certificatePath, privateKeyPath); err != nil {
		controllers.InternalServerError(c, err)
		return
	}

	m, err := pubsub.NewMessageWithJSON(exchanges.Edges, exchanges.RouteV1Invalidation, &messages.V1InvalidationMessageData{
		Domains: []string{domainName},
	})

	if err != nil {
		controllers.InternalServerError(c, err)
		return
	}

	if err := m.Publish(); err != nil {
		controllers.InternalServerError(c, err)
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"deleted": true,
	})
}
