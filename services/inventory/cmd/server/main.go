package main

import (
    "fmt"
    "log"
    "net/http"
)

func main() {
    fmt.Println("Starting service...")
    
    http.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
        w.WriteHeader(http.StatusOK)
        _, _ = w.Write([]byte("OK"))
    })
    
    log.Fatal(http.ListenAndServe(":8080", nil))
}
