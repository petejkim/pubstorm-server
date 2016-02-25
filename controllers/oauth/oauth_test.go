package oauth_test

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"

	"github.com/jinzhu/gorm"
	"github.com/nitrous-io/rise-server/dbconn"
	"github.com/nitrous-io/rise-server/models/oauthtoken"
	"github.com/nitrous-io/rise-server/models/user"
	"github.com/nitrous-io/rise-server/server"
	"github.com/nitrous-io/rise-server/testhelper"
	. "github.com/onsi/ginkgo"
	. "github.com/onsi/ginkgo/extensions/table"
	. "github.com/onsi/gomega"
)

func Test(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "oauth")
}

var _ = Describe("OAuth", func() {
	var (
		db  *gorm.DB
		s   *httptest.Server
		res *http.Response
		err error

		u            *user.User
		clientDBID   uint
		clientID     string
		clientSecret string
	)

	BeforeEach(func() {
		db, err = dbconn.DB()
		Expect(err).To(BeNil())
		testhelper.TruncateTables(db.DB())

		u = &user.User{Email: "foo@example.com", Password: "foobar"}
		err = u.Insert()
		Expect(err).To(BeNil())
		Expect(u.ID).NotTo(BeZero())

		err = db.Table("users").Raw(`
			UPDATE users
			SET confirmed_at = now()
			WHERE id = ?
			RETURNING *;
		`, u.ID).Scan(u).Error
		Expect(err).To(BeNil())

		var queryRes = struct {
			ID           uint
			ClientID     string
			ClientSecret string
		}{}

		err = db.Table("oauth_clients").Raw(`
			INSERT INTO oauth_clients (
				email,
				name,
				organization
			) VALUES (
				'foo@example.com',
				'Foo CLI',
				'FooCorp'
			) RETURNING id, client_id, client_secret;
		`).Scan(&queryRes).Error
		Expect(err).To(BeNil())

		clientDBID = queryRes.ID
		clientID = queryRes.ClientID
		clientSecret = queryRes.ClientSecret
		Expect(clientDBID).NotTo(BeZero())
		Expect(clientID).NotTo(HaveLen(0))
		Expect(clientSecret).NotTo(HaveLen(0))
	})

	AfterEach(func() {
		if res != nil {
			res.Body.Close()
		}
		s.Close()
	})

	Describe("POST /oauth/token", func() {
		doRequest := func(params url.Values, headers http.Header, clientID string, clientSecret string) {
			s = httptest.NewServer(server.New())
			res, err = testhelper.MakeRequest("POST", s.URL+"/oauth/token", params, headers, clientID, clientSecret)
			Expect(err).To(BeNil())
		}

		Context("when the request contains an invalid grant_type", func() {
			BeforeEach(func() {
				doRequest(url.Values{
					"grant_type": {"login_token"},
					"username":   {"foo@example.com"},
					"password":   {"foobar"},
				}, nil, clientID, clientSecret)
			})

			It("returns 400 with 'unsupported_grant_type' error", func() {
				b := &bytes.Buffer{}
				_, err := b.ReadFrom(res.Body)
				Expect(err).To(BeNil())

				Expect(res.StatusCode).To(Equal(http.StatusBadRequest))
				Expect(b.String()).To(MatchJSON(`{
					"error": "unsupported_grant_type",
					"error_description": "grant type \"login_token\" is not supported"
				}`))

				tok := &oauthtoken.OauthToken{}
				err = db.Last(&tok).Error
				Expect(err).To(Equal(gorm.RecordNotFound))
			})
		})

		Context("when a required param is missing", func() {
			DescribeTable("it returns 400 with 'invalid_request' error",
				func(param string) {
					params := url.Values{
						"grant_type": {"password"},
						"username":   {"foo@example.com"},
						"password":   {"foobar"},
					}
					params.Del(param)
					doRequest(params, nil, clientID, clientSecret)

					b := &bytes.Buffer{}
					_, err := b.ReadFrom(res.Body)
					Expect(err).To(BeNil())

					Expect(res.StatusCode).To(Equal(http.StatusBadRequest))
					Expect(b.String()).To(MatchJSON(`{
						"error": "invalid_request",
						"error_description": "\"` + param + `\" is required"
					}`))

					tok := &oauthtoken.OauthToken{}
					err = db.Last(&tok).Error
					Expect(err).To(Equal(gorm.RecordNotFound))
				},
				Entry("grant_type is required", "grant_type"),
				Entry("username is required", "username"),
				Entry("password is required", "password"),
			)
		})

		Context("when the user's credentials are invalid", func() {
			DescribeTable("it returns 400 with 'invalid_grant' error",
				func(param string) {
					params := url.Values{
						"grant_type": {"password"},
						"username":   {"foo@example.com"},
						"password":   {"foobar"},
					}
					params.Set(param, params.Get(param)+"x") // make entry invalid
					doRequest(params, nil, clientID, clientSecret)

					b := &bytes.Buffer{}
					_, err := b.ReadFrom(res.Body)
					Expect(err).To(BeNil())

					Expect(res.StatusCode).To(Equal(http.StatusBadRequest))
					Expect(b.String()).To(MatchJSON(`{
						"error": "invalid_grant",
						"error_description": "user credentials are invalid"
					}`))

					tok := &oauthtoken.OauthToken{}
					err = db.Last(&tok).Error
					Expect(err).To(Equal(gorm.RecordNotFound))
				},
				Entry("username should be valid", "username"),
				Entry("password should be valid", "password"),
			)
		})

		Context("when the user hasn't confirmed email", func() {
			BeforeEach(func() {
				err = db.Table("users").Raw(`
					UPDATE users
					SET confirmed_at = NULL
					WHERE id = ?
					RETURNING *;
				`, u.ID).Scan(u).Error
				Expect(err).To(BeNil())

				doRequest(url.Values{
					"grant_type": {"password"},
					"username":   {"foo@example.com"},
					"password":   {"foobar"},
				}, nil, clientID, clientSecret)
			})

			It("returns 400 with 'invalid_grant' error", func() {
				b := &bytes.Buffer{}
				_, err := b.ReadFrom(res.Body)
				Expect(err).To(BeNil())

				Expect(res.StatusCode).To(Equal(http.StatusBadRequest))
				Expect(b.String()).To(MatchJSON(`{
					"error": "invalid_grant",
					"error_description": "user has not confirmed email address"
				}`))

				tok := &oauthtoken.OauthToken{}
				err = db.Last(&tok).Error
				Expect(err).To(Equal(gorm.RecordNotFound))
			})
		})

		Context("when the client credentials are invalid", func() {
			params := url.Values{
				"grant_type": {"password"},
				"username":   {"foo@example.com"},
				"password":   {"foobar"},
			}

			DescribeTable("it returns 401 with 'invalid_client' error",
				func(doReq func()) {
					doReq()

					b := &bytes.Buffer{}
					_, err := b.ReadFrom(res.Body)
					Expect(err).To(BeNil())

					Expect(res.StatusCode).To(Equal(http.StatusUnauthorized))
					Expect(b.String()).To(MatchJSON(`{
						"error": "invalid_client",
						"error_description": "client credentials are invalid"
					}`))

					tok := &oauthtoken.OauthToken{}
					err = db.Last(&tok).Error
					Expect(err).To(Equal(gorm.RecordNotFound))
				},
				Entry("client id should be valid", func() {
					doRequest(params, nil, "InvalidClientID", clientSecret)
				}),
				Entry("client secret should be valid", func() {
					doRequest(params, nil, clientID, "InvalidClientSecret")
				}),
			)
		})

		Context("when the request is valid", func() {
			BeforeEach(func() {
				doRequest(url.Values{
					"grant_type": {"password"},
					"username":   {"foo@example.com"},
					"password":   {"foobar"},
				}, nil, clientID, clientSecret)
			})

			It("returns 200 with new access token", func() {
				b := &bytes.Buffer{}
				_, err := b.ReadFrom(res.Body)
				Expect(err).To(BeNil())

				tok := &oauthtoken.OauthToken{}
				err = db.Last(&tok).Error
				Expect(err).To(BeNil())

				Expect(tok.UserID).To(Equal(u.ID))
				Expect(tok.OauthClientID).To(Equal(clientDBID))

				Expect(res.StatusCode).To(Equal(http.StatusOK))
				Expect(b.String()).To(MatchJSON(`{
					"access_token": "` + tok.Token + `",
					"token_type": "bearer",
					"client_id": "` + clientID + `"
				}`))
			})
		})
	})
})
