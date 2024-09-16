# connect-go-cache
Easy caching for Connect RPC.

# Usage


```proto
...

service GreetService {
  rpc Greet(GreetRequest) returns (GreetResponse) {
+    // This method can be cached.
+    option idempotency_level = NO_SIDE_EFFECTS;
  }
}
```

```go
...

func main() {
    mux := http.NewServeMux()

    // Configure your caching backend.
    client := redis.NewClient(&redis.Options{Addr: "localhost:6379"})
    cacheMiddleware := connectGoCache.NewCacheMiddleware(client)

+    // Configure Connect RPC Interceptors.
+    interceptors := connect.WithInterceptors(
+        cacheMiddleware.NewReadThroughInboundInterceptor(), // Manages cache interception.
+        cacheMiddleware.NewReadThroughOutboundInterceptor(), // Manages lazily updating the cache on read.
+   )

    // Register the service.
    path, handler := greetv1connect.NewGreetServiceHandler(
        &handlers.GreetServer{},
+       interceptors
    )
    mux.Handle(path, handler)

    // Start the server.
    fmt.Println("Starting server on :8080")
    http.ListenAndServe(
        "localhost:8080",
        h2c.NewHandler(mux, &http2.Server{}),
    )
}
```