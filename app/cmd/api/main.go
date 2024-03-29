package main

import (
	"context"
	redisSource "github.com/redis/go-redis/v9"
	"log"
	"ninja-chat-core-api/config"
	"ninja-chat-core-api/internal/server"
	httpUser "ninja-chat-core-api/internal/user/delivery/http"
	pgRepoUser "ninja-chat-core-api/internal/user/repository"
	redisRepoUser "ninja-chat-core-api/internal/user/repository"
	usecaseUser "ninja-chat-core-api/internal/user/usecase"

	httpConn "ninja-chat-core-api/internal/conn/delivery/http"
	pgRepoConn "ninja-chat-core-api/internal/conn/repository"
	redisRepoConn "ninja-chat-core-api/internal/conn/repository"
	usecaseConn "ninja-chat-core-api/internal/conn/usecase"

	"ninja-chat-core-api/pkg/storage/postgres"
	redisClient "ninja-chat-core-api/pkg/storage/redis"
	"os"
	"os/signal"
	"syscall"

	"ninja-chat-core-api/internal/middleware"

	"github.com/gofiber/fiber/v2"
	"github.com/gofiber/fiber/v2/middleware/logger"
	"github.com/jmoiron/sqlx"
)

func main() {
	viper, err := config.LoadConfig()
	if err != nil {
		log.Fatal(err)
	}

	cfg, err := config.ParseConfig(viper)
	if err != nil {
		log.Fatal(err)
	}
	log.Println("Config loaded")

	ctx := context.Background()
	psqlDB, err := postgres.InitPsqlDB(ctx, cfg)
	if err != nil {
		log.Printf("PostgreSQL error connection: %s", err.Error())
		return
	} else {
		log.Println("PostgreSQL successful connection")
	}
	defer func(psqlDB *sqlx.DB) {
		if err := psqlDB.Close(); err != nil {
			log.Printf("PostgreSQL error close connection: %s", err.Error())
			return
		} else {
			log.Println("PostgreSQL successful close connection")
		}

	}(psqlDB)

	rdb, err := redisClient.InitRedis(cfg)
	if err != nil {
		log.Printf("Redis error connection: %s", err.Error())
		return
	} else {
		log.Println("Redis successful connection")
	}
	defer func(redisClient *redisSource.Client) {
		if err := redisClient.Close(); err != nil {
			log.Printf("Redis unable to close connection: %s", err.Error())
		} else {
			log.Println("Redis successful close connection")
		}

	}(rdb)

	app, deps := mapHandler(cfg, psqlDB, rdb)
	server := server.NewServer(app, deps, cfg)

	if err := server.Run(ctx); err != nil {
		log.Println(err)
		return
	}

	quit := make(chan os.Signal, 1)
	signal.Notify(quit, os.Interrupt, syscall.SIGTERM)
	<-quit

	server.Shutdown()
}

func mapHandler(cfg *config.Config, db *sqlx.DB, rdb *redisSource.Client) (*fiber.App, server.Deps) {
	// create App
	app := fiber.New(fiber.Config{DisableStartupMessage: true})
	app.Use(logger.New())

	// repository
	userPGRepo := pgRepoUser.NewUserPGRepo(cfg, db)
	userRedisRepo := redisRepoUser.NewUserRedisRepo(cfg, rdb)
	connPGRepo := pgRepoConn.NewConnPGRepo(db)
	connRedisRepo := redisRepoConn.NewConnRedisRepo(rdb)

	// usecase
	userUC := usecaseUser.NewUserUsecase(cfg, userPGRepo, userRedisRepo)
	connUC := usecaseConn.NewConnUsecase(cfg, connPGRepo, connRedisRepo)

	// handler
	userHTTP := httpUser.NewUserHandler(cfg, userUC)
	connHTTP := httpConn.NewConnHandler(cfg, connUC)
	// productGRPC := grpcProduct.NewProductHandler(productUC)

	// groups
	apiGroup := app.Group("api")
	userGroup := apiGroup.Group("user")
	connGroup := apiGroup.Group("conn")

	mw := middleware.NewMDWManager(cfg, userUC)

	// routes
	httpUser.MapUserRoutes(userGroup, mw, userHTTP)
	httpConn.MapConnRoutes(connGroup, mw, connHTTP)

	// create grpc dependencyes
	deps := server.Deps{ /* ProductDeps: productGRPC */ }
	return app, deps
}
