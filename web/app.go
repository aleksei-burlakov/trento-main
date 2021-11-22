package web

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"embed"
	"fmt"
	"io/ioutil"
	"net/http"
	"os"
	"time"

	"github.com/gin-contrib/sessions"
	"github.com/gin-contrib/sessions/cookie"
	"github.com/gin-gonic/gin"
	log "github.com/sirupsen/logrus"
	"github.com/spf13/viper"
	"golang.org/x/sync/errgroup"
	"gorm.io/gorm"

	"github.com/trento-project/trento/internal/consul"
	"github.com/trento-project/trento/internal/db"
	"github.com/trento-project/trento/web/datapipeline"
	"github.com/trento-project/trento/web/entities"
	"github.com/trento-project/trento/web/models"
	"github.com/trento-project/trento/web/services"
	"github.com/trento-project/trento/web/services/ara"

	swaggerFiles "github.com/swaggo/files"
	ginSwagger "github.com/swaggo/gin-swagger"
	_ "github.com/trento-project/trento/docs/api" // docs is generated by Swag CLI, you have to import it.
)

//go:embed frontend/assets
var assetsFS embed.FS

//go:embed templates
var templatesFS embed.FS

type App struct {
	config *Config
	Dependencies
}

type Config struct {
	Host          string
	Port          int
	CollectorPort int
	EnablemTLS    bool
	Cert          string
	Key           string
	CA            string
	DBConfig      *db.Config
}
type Dependencies struct {
	consul               consul.Client
	webEngine            *gin.Engine
	collectorEngine      *gin.Engine
	store                cookie.Store
	projectorWorkersPool *datapipeline.ProjectorsWorkerPool
	checksService        services.ChecksService
	subscriptionsService services.SubscriptionsService
	hostsService         services.HostsService
	sapSystemsService    services.SAPSystemsService
	tagsService          services.TagsService
	collectorService     services.CollectorService
	clustersService      services.ClustersService
	hostsNextService     services.HostsNextService
	settingsService      services.SettingsService
}

func DefaultDependencies(config *Config) Dependencies {
	consulClient, _ := consul.DefaultClient()
	webEngine := NewNamedEngine("public")
	collectorEngine := NewNamedEngine("internal")
	store := cookie.NewStore([]byte("secret"))
	mode := os.Getenv(gin.EnvGinMode)

	gin.SetMode(mode)

	db, err := db.InitDB(config.DBConfig)
	if err != nil {
		log.Fatalf("failed to connect database: %s", err)
	}

	if err := MigrateDB(db); err != nil {
		log.Fatalf("failed to migrate database: %s", err)
	}

	projectorRegistry := datapipeline.InitProjectorsRegistry(db)
	projectorWorkersPool := datapipeline.NewProjectorsWorkerPool(projectorRegistry)

	tagsService := services.NewTagsService(db)
	araService := ara.NewAraService(viper.GetString("ara-addr"))
	checksService := services.NewChecksService(araService, db)
	subscriptionsService := services.NewSubscriptionsService(db)
	hostsService := services.NewHostsService(consulClient)
	hostsServiceNext := services.NewHostsNextService(db)
	sapSystemsService := services.NewSAPSystemsService(consulClient)
	clustersService := services.NewClustersService(db, checksService)
	collectorService := services.NewCollectorService(db, projectorWorkersPool.GetChannel())
	settingsService := services.NewSettingsService(db)

	return Dependencies{
		consulClient, webEngine, collectorEngine, store, projectorWorkersPool,
		checksService, subscriptionsService, hostsService, sapSystemsService, tagsService,
		collectorService, clustersService, hostsServiceNext, settingsService,
	}
}

func NewNamedEngine(instance string) *gin.Engine {
	engine := gin.New()
	engine.Use(NewLogHandler(instance, log.StandardLogger()))
	engine.Use(gin.Recovery())
	return engine
}

func MigrateDB(db *gorm.DB) error {
	err := db.AutoMigrate(
		entities.Settings{}, models.Tag{}, models.SelectedChecks{}, models.ConnectionSettings{},
		models.CheckRaw{}, datapipeline.DataCollectedEvent{}, datapipeline.Subscription{},
		models.HostTelemetry{}, entities.Cluster{}, entities.Host{}, entities.HostHeartbeat{},
		entities.SlesSubscription{}, entities.SAPSystemInstance{},
	)

	if err != nil {
		return err
	}

	return nil
}

// shortcut to use default dependencies
func NewApp(config *Config) (*App, error) {
	return NewAppWithDeps(config, DefaultDependencies(config))
}

func NewAppWithDeps(config *Config, deps Dependencies) (*App, error) {
	app := &App{
		config:       config,
		Dependencies: deps,
	}

	if _, err := deps.settingsService.InitializeIdentifier(); err != nil {
		log.Errorf("failed to initialize installation identifier: %s", err)
		return nil, err
	}

	InitAlerts()
	webEngine := deps.webEngine
	webEngine.HTMLRender = NewLayoutRender(templatesFS, "templates/*.tmpl")
	webEngine.Use(ErrorHandler)
	webEngine.Use(sessions.Sessions("session", deps.store))
	webEngine.StaticFS("/static", http.FS(assetsFS))
	webEngine.GET("/", HomeHandler)
	webEngine.GET("/about", NewAboutHandler(deps.subscriptionsService))
	webEngine.GET("/hosts", NewHostListHandler(deps.consul, deps.tagsService))
	webEngine.GET("/hosts-next", NewHostListNextHandler(deps.hostsNextService))
	webEngine.GET("/hosts/:name", NewHostHandler(deps.consul, deps.subscriptionsService))
	webEngine.GET("/catalog", NewChecksCatalogHandler(deps.checksService))
	webEngine.GET("/clusters", NewClusterListHandler(deps.clustersService))
	webEngine.GET("/clusters/:id", NewClusterHandler(deps.consul, deps.checksService))
	webEngine.GET("/sapsystems", NewSAPSystemListHandler(deps.consul, deps.hostsService, deps.sapSystemsService, deps.tagsService))
	webEngine.GET("/sapsystems/:id", NewSAPResourceHandler(deps.hostsService, deps.sapSystemsService))
	webEngine.GET("/databases", NewHanaDatabaseListHandler(deps.consul, deps.hostsService, deps.sapSystemsService, deps.tagsService))
	webEngine.GET("/databases/:id", NewSAPResourceHandler(deps.hostsService, deps.sapSystemsService))

	apiGroup := webEngine.Group("/api")
	{
		apiGroup.GET("/docs/*any", ginSwagger.WrapHandler(swaggerFiles.Handler))
		apiGroup.GET("/ping", ApiPingHandler)
		apiGroup.GET("/tags", ApiListTag(deps.tagsService))
		apiGroup.POST("/hosts/:name/tags", ApiHostCreateTagHandler(deps.consul, deps.tagsService))
		apiGroup.DELETE("/hosts/:name/tags/:tag", ApiHostDeleteTagHandler(deps.consul, deps.tagsService))
		apiGroup.POST("/clusters/:id/tags", ApiClusterCreateTagHandler(deps.consul, deps.tagsService))
		apiGroup.DELETE("/clusters/:id/tags/:tag", ApiClusterDeleteTagHandler(deps.consul, deps.tagsService))
		apiGroup.GET("/clusters/:cluster_id/results", ApiClusterCheckResultsHandler(deps.consul, deps.checksService))
		apiGroup.POST("/sapsystems/:id/tags", ApiSAPSystemCreateTagHandler(deps.sapSystemsService, deps.tagsService))
		apiGroup.DELETE("/sapsystems/:id/tags/:tag", ApiSAPSystemDeleteTagHandler(deps.sapSystemsService, deps.tagsService))
		apiGroup.POST("/databases/:id/tags", ApiDatabaseCreateTagHandler(deps.sapSystemsService, deps.tagsService))
		apiGroup.DELETE("/databases/:id/tags/:tag", ApiDatabaseDeleteTagHandler(deps.sapSystemsService, deps.tagsService))
		apiGroup.GET("/checks/:id/settings", ApiCheckGetSettingsByIdHandler(deps.consul, deps.checksService))
		apiGroup.POST("/checks/:id/settings", ApiCheckCreateSettingsByIdHandler(deps.checksService))
		apiGroup.PUT("/checks/catalog", ApiCreateChecksCatalogHandler(deps.checksService))
		apiGroup.GET("/checks/catalog", ApiChecksCatalogHandler(deps.checksService))
	}

	collectorEngine := deps.collectorEngine
	collectorEngine.POST("/api/collect", ApiCollectDataHandler(deps.collectorService))
	collectorEngine.POST("/api/hosts/:id/heartbeat", ApiHostHeartbeatHandler(deps.hostsNextService))
	collectorEngine.GET("/api/ping", ApiPingHandler)

	return app, nil
}

func (a *App) Start(ctx context.Context) error {
	webServer := &http.Server{
		Addr:           fmt.Sprintf("%s:%d", a.config.Host, a.config.Port),
		Handler:        a.webEngine,
		ReadTimeout:    10 * time.Second,
		WriteTimeout:   10 * time.Second,
		MaxHeaderBytes: 1 << 20,
	}

	var tlsConfig *tls.Config
	var err error

	if a.config.EnablemTLS {
		tlsConfig, err = getTLSConfig(a.config.Cert, a.config.Key, a.config.CA)
		if err != nil {
			return err
		}
	}

	collectorServer := &http.Server{
		Addr:           fmt.Sprintf("%s:%d", a.config.Host, a.config.CollectorPort),
		Handler:        a.collectorEngine,
		ReadTimeout:    10 * time.Second,
		WriteTimeout:   10 * time.Second,
		MaxHeaderBytes: 1 << 20,
		TLSConfig:      tlsConfig,
	}

	g, ctx := errgroup.WithContext(ctx)

	log.Info("Starting web server")
	g.Go(func() error {
		err := webServer.ListenAndServe()
		if err != nil && err != http.ErrServerClosed {
			return err
		}
		return nil
	})

	log.Info("Starting collector server")
	g.Go(func() error {
		var err error
		if tlsConfig == nil {
			err = collectorServer.ListenAndServe()
		} else {
			err = collectorServer.ListenAndServeTLS("", "")
		}
		if err != nil && err != http.ErrServerClosed {
			return err
		}
		return nil
	})

	g.Go(func() error {
		a.projectorWorkersPool.Run(ctx)
		return nil
	})

	go func() {
		<-ctx.Done()
		log.Info("Web server is shutting down.")
		webServer.Close()
		log.Info("Collector server is shutting down.")
		collectorServer.Close()
	}()

	return g.Wait()
}

func getTLSConfig(cert string, key string, ca string) (*tls.Config, error) {
	caCert, err := ioutil.ReadFile(ca)
	if err != nil {
		return nil, err
	}
	caCertPool := x509.NewCertPool()
	caCertPool.AppendCertsFromPEM(caCert)

	certificate, err := tls.LoadX509KeyPair(cert, key)
	if err != nil {
		return nil, err
	}

	return &tls.Config{
		ClientCAs:    caCertPool,
		ClientAuth:   tls.RequireAndVerifyClientCert,
		Certificates: []tls.Certificate{certificate},
	}, nil
}
