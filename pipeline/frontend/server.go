package frontend

import (
	"context"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"go.uber.org/zap"

	"core/utils"
)

type FrontendServer struct {
	Primary    http.Server
	Redirect   http.Server
	httpClient http.Client
}

func routes() *http.ServeMux {
	mux := http.NewServeMux()
	mux.Handle("/", mainHandler)
	mux.HandleFunc("/setup_complete", setupCompleteHandler)
	return mux
}

// LaunchServer spins up goroutines for primary and redirect listeners.
func (fs *FrontendServer) LaunchServer(secure, unsecure, cert, key string) {
	if PROD {
		// Primary server with HTTPS.
		fs.Primary = http.Server{
			Addr:    secure,
			Handler: routes(),
		}
		go func() {
			if err := fs.Primary.ListenAndServeTLS(cert, key); err != nil {
				utils.AppLog.Error(
					"primary server failed to start",
					zap.Error(err),
				)
				panic(err)
			}
		}()

		// For redirection purposes only.
		fs.Redirect = http.Server{
			Addr:    unsecure,
			Handler: http.HandlerFunc(httpRedirect),
		}
		go func() {
			if err := fs.Redirect.ListenAndServe(); err != nil {
				utils.AppLog.Error(
					"redirect server failed to start",
					zap.Error(err),
				)
				panic(err)
			}
		}()
	} else {
		// Primary server with HTTP - Testing Only. Ngrok Doesn't Play Nice with HTTPS.
		fs.Primary = http.Server{
			Addr:    unsecure,
			Handler: routes(),
		}
		go func() {
			if err := fs.Primary.ListenAndServe(); err != nil {
				utils.AppLog.Error(
					"primary server failed to start",
					zap.Error(err),
				)
				panic(err)
			}
		}()
	}
}

// Start provides live/test LaunchServer with necessary startup information.
func (fs *FrontendServer) Start() {
	stop := make(chan os.Signal, 1)
	signal.Notify(stop, os.Interrupt, syscall.SIGTERM)

	if PROD {
		fs.LaunchServer(
			"10.142.1.0:443",
			"10.142.1.0:80",
			"heupr_io.crt",
			"heupr.key",
		)
	} else {
		fs.LaunchServer(
			"127.0.0.1:8081",
			"127.0.0.1:8080",
			"cert.pem",
			"key.pem",
		)
	}

	<-stop
	utils.AppLog.Info("keyboard interrupt received")
	fs.Stop()
}

// Stop gracefully closes down all server instances.
func (fs *FrontendServer) Stop() {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	fs.Primary.Shutdown(ctx)
	fs.Redirect.Shutdown(ctx)
	utils.AppLog.Info("graceful frontend shutdown")
}
