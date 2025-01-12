package pluginproxy

import (
	"bytes"
	"fmt"
	"io/ioutil"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
	"time"

	"github.com/Seasheller/grafana/pkg/components/securejsondata"

	"golang.org/x/oauth2"
	macaron "gopkg.in/macaron.v1"

	"github.com/Seasheller/grafana/pkg/bus"
	"github.com/Seasheller/grafana/pkg/components/simplejson"
	"github.com/Seasheller/grafana/pkg/infra/log"
	"github.com/Seasheller/grafana/pkg/login/social"
	m "github.com/Seasheller/grafana/pkg/models"
	"github.com/Seasheller/grafana/pkg/plugins"
	"github.com/Seasheller/grafana/pkg/setting"
	"github.com/Seasheller/grafana/pkg/util"
	. "github.com/smartystreets/goconvey/convey"
)

func TestDSRouteRule(t *testing.T) {

	Convey("DataSourceProxy", t, func() {
		Convey("Plugin with routes", func() {
			plugin := &plugins.DataSourcePlugin{
				Routes: []*plugins.AppPluginRoute{
					{
						Path:    "api/v4/",
						Url:     "https://www.google.com",
						ReqRole: m.ROLE_EDITOR,
						Headers: []plugins.AppPluginRouteHeader{
							{Name: "x-header", Content: "my secret {{.SecureJsonData.key}}"},
						},
					},
					{
						Path:    "api/admin",
						Url:     "https://www.google.com",
						ReqRole: m.ROLE_ADMIN,
						Headers: []plugins.AppPluginRouteHeader{
							{Name: "x-header", Content: "my secret {{.SecureJsonData.key}}"},
						},
					},
					{
						Path: "api/anon",
						Url:  "https://www.google.com",
						Headers: []plugins.AppPluginRouteHeader{
							{Name: "x-header", Content: "my secret {{.SecureJsonData.key}}"},
						},
					},
					{
						Path: "api/common",
						Url:  "{{.JsonData.dynamicUrl}}",
						Headers: []plugins.AppPluginRouteHeader{
							{Name: "x-header", Content: "my secret {{.SecureJsonData.key}}"},
						},
					},
				},
			}

			setting.SecretKey = "password" //nolint:goconst
			key, _ := util.Encrypt([]byte("123"), "password")

			ds := &m.DataSource{
				JsonData: simplejson.NewFromAny(map[string]interface{}{
					"clientId":   "asd",
					"dynamicUrl": "https://dynamic.grafana.com",
				}),
				SecureJsonData: map[string][]byte{
					"key": key,
				},
			}

			req, _ := http.NewRequest("GET", "http://localhost/asd", nil)
			ctx := &m.ReqContext{
				Context: &macaron.Context{
					Req: macaron.Request{Request: req},
				},
				SignedInUser: &m.SignedInUser{OrgRole: m.ROLE_EDITOR},
			}

			Convey("When matching route path", func() {
				proxy := NewDataSourceProxy(ds, plugin, ctx, "api/v4/some/method", &setting.Cfg{})
				proxy.route = plugin.Routes[0]
				ApplyRoute(proxy.ctx.Req.Context(), req, proxy.proxyPath, proxy.route, proxy.ds)

				Convey("should add headers and update url", func() {
					So(req.URL.String(), ShouldEqual, "https://www.google.com/some/method")
					So(req.Header.Get("x-header"), ShouldEqual, "my secret 123")
				})
			})

			Convey("When matching route path and has dynamic url", func() {
				proxy := NewDataSourceProxy(ds, plugin, ctx, "api/common/some/method", &setting.Cfg{})
				proxy.route = plugin.Routes[3]
				ApplyRoute(proxy.ctx.Req.Context(), req, proxy.proxyPath, proxy.route, proxy.ds)

				Convey("should add headers and interpolate the url", func() {
					So(req.URL.String(), ShouldEqual, "https://dynamic.grafana.com/some/method")
					So(req.Header.Get("x-header"), ShouldEqual, "my secret 123")
				})
			})

			Convey("Validating request", func() {
				Convey("plugin route with valid role", func() {
					proxy := NewDataSourceProxy(ds, plugin, ctx, "api/v4/some/method", &setting.Cfg{})
					err := proxy.validateRequest()
					So(err, ShouldBeNil)
				})

				Convey("plugin route with admin role and user is editor", func() {
					proxy := NewDataSourceProxy(ds, plugin, ctx, "api/admin", &setting.Cfg{})
					err := proxy.validateRequest()
					So(err, ShouldNotBeNil)
				})

				Convey("plugin route with admin role and user is admin", func() {
					ctx.SignedInUser.OrgRole = m.ROLE_ADMIN
					proxy := NewDataSourceProxy(ds, plugin, ctx, "api/admin", &setting.Cfg{})
					err := proxy.validateRequest()
					So(err, ShouldBeNil)
				})
			})
		})

		Convey("Plugin with multiple routes for token auth", func() {
			plugin := &plugins.DataSourcePlugin{
				Routes: []*plugins.AppPluginRoute{
					{
						Path: "pathwithtoken1",
						Url:  "https://api.nr1.io/some/path",
						TokenAuth: &plugins.JwtTokenAuth{
							Url: "https://login.server.com/{{.JsonData.tenantId}}/oauth2/token",
							Params: map[string]string{
								"grant_type":    "client_credentials",
								"client_id":     "{{.JsonData.clientId}}",
								"client_secret": "{{.SecureJsonData.clientSecret}}",
								"resource":      "https://api.nr1.io",
							},
						},
					},
					{
						Path: "pathwithtoken2",
						Url:  "https://api.nr2.io/some/path",
						TokenAuth: &plugins.JwtTokenAuth{
							Url: "https://login.server.com/{{.JsonData.tenantId}}/oauth2/token",
							Params: map[string]string{
								"grant_type":    "client_credentials",
								"client_id":     "{{.JsonData.clientId}}",
								"client_secret": "{{.SecureJsonData.clientSecret}}",
								"resource":      "https://api.nr2.io",
							},
						},
					},
				},
			}

			setting.SecretKey = "password"
			key, _ := util.Encrypt([]byte("123"), "password")

			ds := &m.DataSource{
				JsonData: simplejson.NewFromAny(map[string]interface{}{
					"clientId": "asd",
					"tenantId": "mytenantId",
				}),
				SecureJsonData: map[string][]byte{
					"clientSecret": key,
				},
			}

			req, _ := http.NewRequest("GET", "http://localhost/asd", nil)
			ctx := &m.ReqContext{
				Context: &macaron.Context{
					Req: macaron.Request{Request: req},
				},
				SignedInUser: &m.SignedInUser{OrgRole: m.ROLE_EDITOR},
			}

			Convey("When creating and caching access tokens", func() {
				var authorizationHeaderCall1 string
				var authorizationHeaderCall2 string

				Convey("first call should add authorization header with access token", func() {
					json, err := ioutil.ReadFile("./test-data/access-token-1.json")
					So(err, ShouldBeNil)

					client = newFakeHTTPClient(json)
					proxy1 := NewDataSourceProxy(ds, plugin, ctx, "pathwithtoken1", &setting.Cfg{})
					proxy1.route = plugin.Routes[0]
					ApplyRoute(proxy1.ctx.Req.Context(), req, proxy1.proxyPath, proxy1.route, proxy1.ds)

					authorizationHeaderCall1 = req.Header.Get("Authorization")
					So(req.URL.String(), ShouldEqual, "https://api.nr1.io/some/path")
					So(authorizationHeaderCall1, ShouldStartWith, "Bearer eyJ0e")

					Convey("second call to another route should add a different access token", func() {
						json2, err := ioutil.ReadFile("./test-data/access-token-2.json")
						So(err, ShouldBeNil)

						req, _ := http.NewRequest("GET", "http://localhost/asd", nil)
						client = newFakeHTTPClient(json2)
						proxy2 := NewDataSourceProxy(ds, plugin, ctx, "pathwithtoken2", &setting.Cfg{})
						proxy2.route = plugin.Routes[1]
						ApplyRoute(proxy2.ctx.Req.Context(), req, proxy2.proxyPath, proxy2.route, proxy2.ds)

						authorizationHeaderCall2 = req.Header.Get("Authorization")

						So(req.URL.String(), ShouldEqual, "https://api.nr2.io/some/path")
						So(authorizationHeaderCall1, ShouldStartWith, "Bearer eyJ0e")
						So(authorizationHeaderCall2, ShouldStartWith, "Bearer eyJ0e")
						So(authorizationHeaderCall2, ShouldNotEqual, authorizationHeaderCall1)

						Convey("third call to first route should add cached access token", func() {
							req, _ := http.NewRequest("GET", "http://localhost/asd", nil)

							client = newFakeHTTPClient([]byte{})
							proxy3 := NewDataSourceProxy(ds, plugin, ctx, "pathwithtoken1", &setting.Cfg{})
							proxy3.route = plugin.Routes[0]
							ApplyRoute(proxy3.ctx.Req.Context(), req, proxy3.proxyPath, proxy3.route, proxy3.ds)

							authorizationHeaderCall3 := req.Header.Get("Authorization")
							So(req.URL.String(), ShouldEqual, "https://api.nr1.io/some/path")
							So(authorizationHeaderCall1, ShouldStartWith, "Bearer eyJ0e")
							So(authorizationHeaderCall3, ShouldStartWith, "Bearer eyJ0e")
							So(authorizationHeaderCall3, ShouldEqual, authorizationHeaderCall1)
						})
					})
				})
			})
		})

		Convey("When proxying graphite", func() {
			setting.BuildVersion = "5.3.0"
			plugin := &plugins.DataSourcePlugin{}
			ds := &m.DataSource{Url: "htttp://graphite:8080", Type: m.DS_GRAPHITE}
			ctx := &m.ReqContext{}

			proxy := NewDataSourceProxy(ds, plugin, ctx, "/render", &setting.Cfg{})
			req, err := http.NewRequest(http.MethodGet, "http://grafana.com/sub", nil)
			So(err, ShouldBeNil)

			proxy.getDirector()(req)

			Convey("Can translate request url and path", func() {
				So(req.URL.Host, ShouldEqual, "graphite:8080")
				So(req.URL.Path, ShouldEqual, "/render")
				So(req.Header.Get("User-Agent"), ShouldEqual, "Grafana/5.3.0")
			})
		})

		Convey("When proxying InfluxDB", func() {
			plugin := &plugins.DataSourcePlugin{}

			ds := &m.DataSource{
				Type:     m.DS_INFLUXDB_08,
				Url:      "http://influxdb:8083",
				Database: "site",
				User:     "user",
				Password: "password",
			}

			ctx := &m.ReqContext{}
			proxy := NewDataSourceProxy(ds, plugin, ctx, "", &setting.Cfg{})

			req, err := http.NewRequest(http.MethodGet, "http://grafana.com/sub", nil)
			So(err, ShouldBeNil)

			proxy.getDirector()(req)

			Convey("Should add db to url", func() {
				So(req.URL.Path, ShouldEqual, "/db/site/")
			})
		})

		Convey("When proxying a data source with no keepCookies specified", func() {
			plugin := &plugins.DataSourcePlugin{}

			json, _ := simplejson.NewJson([]byte(`{"keepCookies": []}`))

			ds := &m.DataSource{
				Type:     m.DS_GRAPHITE,
				Url:      "http://graphite:8086",
				JsonData: json,
			}

			ctx := &m.ReqContext{}
			proxy := NewDataSourceProxy(ds, plugin, ctx, "", &setting.Cfg{})

			requestURL, _ := url.Parse("http://grafana.com/sub")
			req := http.Request{URL: requestURL, Header: make(http.Header)}
			cookies := "grafana_user=admin; grafana_remember=99; grafana_sess=11; JSESSION_ID=test"
			req.Header.Set("Cookie", cookies)

			proxy.getDirector()(&req)

			Convey("Should clear all cookies", func() {
				So(req.Header.Get("Cookie"), ShouldEqual, "")
			})
		})

		Convey("When proxying a data source with keep cookies specified", func() {
			plugin := &plugins.DataSourcePlugin{}

			json, _ := simplejson.NewJson([]byte(`{"keepCookies": ["JSESSION_ID"]}`))

			ds := &m.DataSource{
				Type:     m.DS_GRAPHITE,
				Url:      "http://graphite:8086",
				JsonData: json,
			}

			ctx := &m.ReqContext{}
			proxy := NewDataSourceProxy(ds, plugin, ctx, "", &setting.Cfg{})

			requestURL, _ := url.Parse("http://grafana.com/sub")
			req := http.Request{URL: requestURL, Header: make(http.Header)}
			cookies := "grafana_user=admin; grafana_remember=99; grafana_sess=11; JSESSION_ID=test"
			req.Header.Set("Cookie", cookies)

			proxy.getDirector()(&req)

			Convey("Should keep named cookies", func() {
				So(req.Header.Get("Cookie"), ShouldEqual, "JSESSION_ID=test")
			})
		})

		Convey("When proxying a data source with custom headers specified", func() {
			plugin := &plugins.DataSourcePlugin{}

			encryptedData, err := util.Encrypt([]byte(`Bearer xf5yhfkpsnmgo`), setting.SecretKey)
			ds := &m.DataSource{
				Type: m.DS_PROMETHEUS,
				Url:  "http://prometheus:9090",
				JsonData: simplejson.NewFromAny(map[string]interface{}{
					"httpHeaderName1": "Authorization",
				}),
				SecureJsonData: map[string][]byte{
					"httpHeaderValue1": encryptedData,
				},
			}

			ctx := &m.ReqContext{}
			proxy := NewDataSourceProxy(ds, plugin, ctx, "", &setting.Cfg{})

			requestURL, _ := url.Parse("http://grafana.com/sub")
			req := http.Request{URL: requestURL, Header: make(http.Header)}
			proxy.getDirector()(&req)

			if err != nil {
				log.Fatal(4, err.Error())
			}

			Convey("Match header value after decryption", func() {
				So(req.Header.Get("Authorization"), ShouldEqual, "Bearer xf5yhfkpsnmgo")
			})
		})

		Convey("When proxying a custom datasource", func() {
			plugin := &plugins.DataSourcePlugin{}
			ds := &m.DataSource{
				Type: "custom-datasource",
				Url:  "http://host/root/",
			}
			ctx := &m.ReqContext{}
			proxy := NewDataSourceProxy(ds, plugin, ctx, "/path/to/folder/", &setting.Cfg{})
			req, err := http.NewRequest(http.MethodGet, "http://grafana.com/sub", nil)
			req.Header.Add("Origin", "grafana.com")
			req.Header.Add("Referer", "grafana.com")
			req.Header.Add("X-Canary", "stillthere")
			So(err, ShouldBeNil)

			proxy.getDirector()(req)

			Convey("Should keep user request (including trailing slash)", func() {
				So(req.URL.String(), ShouldEqual, "http://host/root/path/to/folder/")
			})

			Convey("Origin and Referer headers should be dropped", func() {
				So(req.Header.Get("Origin"), ShouldEqual, "")
				So(req.Header.Get("Referer"), ShouldEqual, "")
				So(req.Header.Get("X-Canary"), ShouldEqual, "stillthere")
			})
		})

		Convey("When proxying a datasource that has oauth token pass-thru enabled", func() {
			social.SocialMap["generic_oauth"] = &social.SocialGenericOAuth{
				SocialBase: &social.SocialBase{
					Config: &oauth2.Config{},
				},
			}

			bus.AddHandler("test", func(query *m.GetAuthInfoQuery) error {
				query.Result = &m.UserAuth{
					Id:                1,
					UserId:            1,
					AuthModule:        "generic_oauth",
					OAuthAccessToken:  "testtoken",
					OAuthRefreshToken: "testrefreshtoken",
					OAuthTokenType:    "Bearer",
					OAuthExpiry:       time.Now().AddDate(0, 0, 1),
				}
				return nil
			})

			plugin := &plugins.DataSourcePlugin{}
			ds := &m.DataSource{
				Type: "custom-datasource",
				Url:  "http://host/root/",
				JsonData: simplejson.NewFromAny(map[string]interface{}{
					"oauthPassThru": true,
				}),
			}

			req, _ := http.NewRequest("GET", "http://localhost/asd", nil)
			ctx := &m.ReqContext{
				SignedInUser: &m.SignedInUser{UserId: 1},
				Context: &macaron.Context{
					Req: macaron.Request{Request: req},
				},
			}
			proxy := NewDataSourceProxy(ds, plugin, ctx, "/path/to/folder/", &setting.Cfg{})
			req, err := http.NewRequest(http.MethodGet, "http://grafana.com/sub", nil)

			So(err, ShouldBeNil)

			proxy.getDirector()(req)

			Convey("Should have access token in header", func() {
				So(req.Header.Get("Authorization"), ShouldEqual, fmt.Sprintf("%s %s", "Bearer", "testtoken"))
			})
		})

		Convey("When SendUserHeader config is enabled", func() {
			req := getDatasourceProxiedRequest(
				&m.ReqContext{
					SignedInUser: &m.SignedInUser{
						Login: "test_user",
					},
				},
				&setting.Cfg{SendUserHeader: true},
			)
			Convey("Should add header with username", func() {
				So(req.Header.Get("X-Grafana-User"), ShouldEqual, "test_user")
			})
		})

		Convey("When SendUserHeader config is disabled", func() {
			req := getDatasourceProxiedRequest(
				&m.ReqContext{
					SignedInUser: &m.SignedInUser{
						Login: "test_user",
					},
				},
				&setting.Cfg{SendUserHeader: false},
			)
			Convey("Should not add header with username", func() {
				// Get will return empty string even if header is not set
				So(req.Header.Get("X-Grafana-User"), ShouldEqual, "")
			})
		})

		Convey("When SendUserHeader config is enabled but user is anonymous", func() {
			req := getDatasourceProxiedRequest(
				&m.ReqContext{
					SignedInUser: &m.SignedInUser{IsAnonymous: true},
				},
				&setting.Cfg{SendUserHeader: true},
			)
			Convey("Should not add header with username", func() {
				// Get will return empty string even if header is not set
				So(req.Header.Get("X-Grafana-User"), ShouldEqual, "")
			})
		})

		Convey("When proxying data source proxy should handle authentication", func() {
			tests := []*Test{
				createAuthTest(m.DS_INFLUXDB_08, AUTHTYPE_PASSWORD, AUTHCHECK_QUERY, false),
				createAuthTest(m.DS_INFLUXDB_08, AUTHTYPE_PASSWORD, AUTHCHECK_QUERY, true),
				createAuthTest(m.DS_INFLUXDB, AUTHTYPE_PASSWORD, AUTHCHECK_HEADER, true),
				createAuthTest(m.DS_INFLUXDB, AUTHTYPE_PASSWORD, AUTHCHECK_HEADER, false),
				createAuthTest(m.DS_INFLUXDB, AUTHTYPE_BASIC, AUTHCHECK_HEADER, true),
				createAuthTest(m.DS_INFLUXDB, AUTHTYPE_BASIC, AUTHCHECK_HEADER, false),

				// These two should be enough for any other datasource at the moment. Proxy has special handling
				// only for Influx, others have the same path and only BasicAuth. Non BasicAuth datasources
				// do not go through proxy but through TSDB API which is not tested here.
				createAuthTest(m.DS_ES, AUTHTYPE_BASIC, AUTHCHECK_HEADER, false),
				createAuthTest(m.DS_ES, AUTHTYPE_BASIC, AUTHCHECK_HEADER, true),
			}
			for _, test := range tests {
				runDatasourceAuthTest(test)
			}
		})

		Convey("HandleRequest()", func() {
			backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				http.SetCookie(w, &http.Cookie{Name: "flavor", Value: "chocolateChip"})
				w.WriteHeader(200)
				w.Write([]byte("I am the backend"))
			}))
			defer backend.Close()

			plugin := &plugins.DataSourcePlugin{}
			ds := &m.DataSource{Url: backend.URL, Type: m.DS_GRAPHITE}

			responseRecorder := &CloseNotifierResponseRecorder{
				ResponseRecorder: httptest.NewRecorder(),
			}
			defer responseRecorder.Close()

			setupCtx := func(fn func(http.ResponseWriter)) *m.ReqContext {
				responseWriter := macaron.NewResponseWriter("GET", responseRecorder)
				if fn != nil {
					fn(responseWriter)
				}

				return &m.ReqContext{
					SignedInUser: &m.SignedInUser{},
					Context: &macaron.Context{
						Req: macaron.Request{
							Request: httptest.NewRequest("GET", "/render", nil),
						},
						Resp: responseWriter,
					},
				}
			}

			Convey("When response header Set-Cookie is not set should remove proxied Set-Cookie header", func() {
				ctx := setupCtx(nil)
				proxy := NewDataSourceProxy(ds, plugin, ctx, "/render", &setting.Cfg{})
				proxy.HandleRequest()
				So(proxy.ctx.Resp.Header().Get("Set-Cookie"), ShouldBeEmpty)
			})

			Convey("When response header Set-Cookie is set should remove proxied Set-Cookie header and restore the original Set-Cookie header", func() {
				ctx := setupCtx(func(w http.ResponseWriter) {
					w.Header().Set("Set-Cookie", "important_cookie=important_value")
				})
				proxy := NewDataSourceProxy(ds, plugin, ctx, "/render", &setting.Cfg{})
				proxy.HandleRequest()
				So(proxy.ctx.Resp.Header().Get("Set-Cookie"), ShouldEqual, "important_cookie=important_value")
			})
		})
	})
}

type CloseNotifierResponseRecorder struct {
	*httptest.ResponseRecorder
	closeChan chan bool
}

func (r *CloseNotifierResponseRecorder) CloseNotify() <-chan bool {
	r.closeChan = make(chan bool)
	return r.closeChan
}

func (r *CloseNotifierResponseRecorder) Close() {
	close(r.closeChan)
}

// getDatasourceProxiedRequest is a helper for easier setup of tests based on global config and ReqContext.
func getDatasourceProxiedRequest(ctx *m.ReqContext, cfg *setting.Cfg) *http.Request {
	plugin := &plugins.DataSourcePlugin{}

	ds := &m.DataSource{
		Type: "custom",
		Url:  "http://host/root/",
	}

	proxy := NewDataSourceProxy(ds, plugin, ctx, "", cfg)
	req, err := http.NewRequest(http.MethodGet, "http://grafana.com/sub", nil)
	So(err, ShouldBeNil)

	proxy.getDirector()(req)
	return req
}

type httpClientStub struct {
	fakeBody []byte
}

func (c *httpClientStub) Do(req *http.Request) (*http.Response, error) {
	bodyJSON, _ := simplejson.NewJson(c.fakeBody)
	_, passedTokenCacheTest := bodyJSON.CheckGet("expires_on")
	So(passedTokenCacheTest, ShouldBeTrue)

	bodyJSON.Set("expires_on", fmt.Sprint(time.Now().Add(time.Second*60).Unix()))
	body, _ := bodyJSON.MarshalJSON()
	resp := &http.Response{
		Body: ioutil.NopCloser(bytes.NewReader(body)),
	}

	return resp, nil
}

func newFakeHTTPClient(fakeBody []byte) httpClient {
	return &httpClientStub{
		fakeBody: fakeBody,
	}
}

type Test struct {
	datasource *m.DataSource
	checkReq   func(req *http.Request)
}

const (
	AUTHTYPE_PASSWORD = "password"
	AUTHTYPE_BASIC    = "basic"
)

const (
	AUTHCHECK_QUERY  = "query"
	AUTHCHECK_HEADER = "header"
)

func createAuthTest(dsType string, authType string, authCheck string, useSecureJsonData bool) *Test {
	// Basic user:password
	base64AthHeader := "Basic dXNlcjpwYXNzd29yZA=="

	test := &Test{
		datasource: &m.DataSource{
			Type:     dsType,
			JsonData: simplejson.New(),
		},
	}
	var message string
	if authType == AUTHTYPE_PASSWORD {
		message = fmt.Sprintf("%v should add username and password", dsType)
		test.datasource.User = "user"
		if useSecureJsonData {
			test.datasource.SecureJsonData = securejsondata.GetEncryptedJsonData(map[string]string{
				"password": "password",
			})
		} else {
			test.datasource.Password = "password"
		}
	} else {
		message = fmt.Sprintf("%v should add basic auth username and password", dsType)
		test.datasource.BasicAuth = true
		test.datasource.BasicAuthUser = "user"
		if useSecureJsonData {
			test.datasource.SecureJsonData = securejsondata.GetEncryptedJsonData(map[string]string{
				"basicAuthPassword": "password",
			})
		} else {
			test.datasource.BasicAuthPassword = "password"
		}
	}

	if useSecureJsonData {
		message += " from securejsondata"
	}

	if authCheck == AUTHCHECK_QUERY {
		message += " to query params"
		test.checkReq = func(req *http.Request) {
			Convey(message, func() {
				queryVals := req.URL.Query()
				So(queryVals["u"][0], ShouldEqual, "user")
				So(queryVals["p"][0], ShouldEqual, "password")
			})
		}
	} else {
		message += " to auth header"
		test.checkReq = func(req *http.Request) {
			Convey(message, func() {
				So(req.Header.Get("Authorization"), ShouldEqual, base64AthHeader)
			})
		}
	}

	return test
}

func runDatasourceAuthTest(test *Test) {
	plugin := &plugins.DataSourcePlugin{}
	ctx := &m.ReqContext{}
	proxy := NewDataSourceProxy(test.datasource, plugin, ctx, "", &setting.Cfg{})

	req, err := http.NewRequest(http.MethodGet, "http://grafana.com/sub", nil)
	So(err, ShouldBeNil)

	proxy.getDirector()(req)

	test.checkReq(req)
}
