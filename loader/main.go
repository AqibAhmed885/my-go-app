package main

import (
	"fmt"
	"io"
	"os"

	"ariga.io/atlas-provider-gorm/gormschema"
	"github.com/AqibAhmed885/my-go-app/internal/models"
)

func main() {
	stmts, err := gormschema.New("postgres").Load(&models.User{})
	if err != nil {
		fmt.Fprintf(os.Stderr, "failed to load GORM schema: %v\n", err)
		os.Exit(1)
	}

	_, _ = io.WriteString(os.Stdout, stmts)
}
