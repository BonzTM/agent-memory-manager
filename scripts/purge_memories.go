// +build ignore

package main

import (
	"database/sql"
	"fmt"
	"log"

	_ "github.com/mattn/go-sqlite3"
)

func main() {
	db, err := sql.Open("sqlite3", "/home/joshd/.amm/amm.db")
	if err != nil {
		log.Fatal(err)
	}
	defer db.Close()

	var count int
	db.QueryRow("SELECT COUNT(*) FROM memories WHERE status = ?", "active").Scan(&count)
	fmt.Printf("Active memories before purge: %d\n", count)

	res, err := db.Exec("UPDATE memories SET status = ? WHERE status = ?", "retracted", "active")
	if err != nil {
		log.Fatal(err)
	}
	affected, _ := res.RowsAffected()
	fmt.Printf("Retracted: %d memories\n", affected)

	res2, err := db.Exec("UPDATE memories SET status = ? WHERE status = ?", "retracted", "superseded")
	if err != nil {
		log.Fatal(err)
	}
	affected2, _ := res2.RowsAffected()
	fmt.Printf("Retracted (previously superseded): %d memories\n", affected2)

	db.QueryRow("SELECT COUNT(*) FROM memories WHERE status = ?", "active").Scan(&count)
	fmt.Printf("Active memories after purge: %d\n", count)

	res3, err := db.Exec("DELETE FROM embeddings WHERE object_kind = ?", "memory")
	if err != nil {
		log.Fatal(err)
	}
	affected3, _ := res3.RowsAffected()
	fmt.Printf("Cleared memory embeddings: %d\n", affected3)
}
