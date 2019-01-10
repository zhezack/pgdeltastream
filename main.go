package main

import (
	"os"
	"strconv"

	log "github.com/sirupsen/logrus"
	"github.com/zhezack/pgdeltastream/db"
	"github.com/zhezack/pgdeltastream/server"
)

func main() {
	dbName := os.Getenv("DBNAME")
	pgUser := os.Getenv("PGUSER")
	pgPass := os.Getenv("PGPASS")
	pgHost := os.Getenv("PGHOST")
	pgPort, err := strconv.Atoi(os.Getenv("PGPORT"))
	if err != nil {
		log.Error(err.Error())
	}
	serverHost := os.Getenv("SERVERHOST")
	serverPort, err := strconv.Atoi(os.Getenv("SERVERPORT"))
	if err != nil {
		log.Error(err.Error())
	}
	db.CreateConfig(dbName, pgUser, pgPass, pgHost, pgPort)

	log.Infof("Starting server for database %s; serving at %s:%d", dbName, serverHost, serverPort)
	server.StartServer(serverHost, serverPort)
}
