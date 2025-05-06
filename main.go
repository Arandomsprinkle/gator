package config

import (
	"fmt"
	"log"

	"github.com/arandomsprinkle/gator/internal/config"
)

func main() {
	cfg, err := config.Read()
	if err != nil {
		log.Fatal(err)
	}
	err = cfg.SetUser("Arandomsprinkle")
	if err != nil {
		log.Fatal(err)
	}
	newcfg, err := config.Read()
	fmt.Println(newcfg)
}
