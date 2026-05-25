package api

import (
	"os"
)

func dbTestDSN() string {
	host := os.Getenv("ACB_PG_HOST")
	if host == "" {
		host = "localhost"
	}
	port := os.Getenv("ACB_PG_PORT")
	if port == "" {
		port = "5433"
	}
	user := os.Getenv("ACB_PG_USER")
	if user == "" {
		user = "acb"
	}
	password := os.Getenv("ACB_PG_PASSWORD")
	if password == "" {
		password = "acb-secure-pg-pass-2026"
	}
	database := os.Getenv("ACB_PG_DATABASE")
	if database == "" {
		database = "acb"
	}
	return "host=" + host + " port=" + port + " user=" + user + " password=" + password + " dbname=" + database + " sslmode=disable"
}