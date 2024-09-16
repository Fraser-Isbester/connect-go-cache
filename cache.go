package cache

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"connectrpc.com/connect"
	"github.com/go-redis/redis/v8"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/reflect/protoreflect"
	"google.golang.org/protobuf/types/descriptorpb"
	"google.golang.org/protobuf/types/dynamicpb"
)

// CacheMiddleware struct holds the Redis client
type CacheMiddleware struct {
	Client   *redis.Client
	CacheTTL time.Duration
}

// NewCacheMiddleware initializes the caching middleware with a Redis client
func NewCacheMiddleware(redisClient *redis.Client) *CacheMiddleware {
	return &CacheMiddleware{
		Client:   redisClient,
		CacheTTL: 5 * time.Second,
	}
}

func (cm *CacheMiddleware) NewReadThroughInboundInterceptor() connect.UnaryInterceptorFunc {
	return func(next connect.UnaryFunc) connect.UnaryFunc {
		return func(ctx context.Context, req connect.AnyRequest) (connect.AnyResponse, error) {

			methodDescriptor := req.Spec().Schema.(protoreflect.MethodDescriptor)
			if getMethodIdempotencyLevel(methodDescriptor) != descriptorpb.MethodOptions_NO_SIDE_EFFECTS {
				return next(ctx, req)
			}

			// Skip caching if the Cache-Control header is set to no-cache
			if req.Header().Get("Cache-Control") == "no-cache" {
				return next(ctx, req)
			}

			// Fetch Cache Key
			cacheKey := getCacheKey(req)

			// Check if the request is already cached
			cachedData, err := cm.Client.Get(ctx, cacheKey).Result()
			if err == redis.Nil {
				return next(ctx, req)
			} else if err != nil {
				return next(ctx, req)
			}

			// Cache hit
			messageDescriptor := methodDescriptor.Output()
			dynamicOutput := dynamicpb.NewMessage(messageDescriptor)
			err = proto.Unmarshal([]byte(cachedData), dynamicOutput)
			if err != nil {
				return nil, fmt.Errorf("failed to unmarshal cached response: %v", err)
			}

			// Create a new response with the cached data
			response := connect.NewResponse(dynamicOutput)

			// Set cache headers for a hit
			response.Header().Set("X-Cache", "HIT")
			response.Header().Set("X-Cache-Key", cacheKey)

			return response, nil
		}
	}
}

func (cm *CacheMiddleware) NewReadThroughOutboundInterceptor() connect.UnaryInterceptorFunc {
	return func(next connect.UnaryFunc) connect.UnaryFunc {
		return func(ctx context.Context, req connect.AnyRequest) (connect.AnyResponse, error) {
			// Call the next function to get the response from the handler
			resp, err := next(ctx, req)
			if err != nil {
				return nil, err
			}

			methodDescriptor := req.Spec().Schema.(protoreflect.MethodDescriptor)
			if getMethodIdempotencyLevel(methodDescriptor) != descriptorpb.MethodOptions_NO_SIDE_EFFECTS {
				return next(ctx, req)
			}

			// Cache the response for future requests using proto.Marshal
			cacheKey := getCacheKey(req)
			data, marshalErr := proto.Marshal(resp.Any().(proto.Message)) // Use proto.Marshal for Protobuf
			if marshalErr != nil {
				return nil, fmt.Errorf("failed to marshal response for caching: %v", marshalErr)
			}

			// Set cache with the response data
			err = cm.Client.Set(ctx, cacheKey, data, cm.CacheTTL).Err()
			if err != nil {
				return nil, fmt.Errorf("failed to set cache: %v", err)
			}

			return resp, nil
		}
	}
}

func getMethodIdempotencyLevel(methodDescriptor protoreflect.MethodDescriptor) descriptorpb.MethodOptions_IdempotencyLevel {
	methodOptions := methodDescriptor.Options().(*descriptorpb.MethodOptions)
	return methodOptions.GetIdempotencyLevel()
}

// getCacheKey generates a cache key using the procedure path
func getCacheKey(req connect.AnyRequest) string {
	trimmedProc := strings.TrimPrefix(req.Spec().Procedure, "/")
	return fmt.Sprintf("cache:%s", trimmedProc)
}

// NewRedisClient creates a new Redis client
func NewRedisClient(addr, password string) *redis.Client {
	rdb := redis.NewClient(&redis.Options{
		Addr:     addr,
		Password: password,
		DB:       0,
	})

	// Check connection
	_, err := rdb.Ping(context.Background()).Result()
	if err != nil {
		fmt.Printf("Could not connect to Redis: %v\n", err)
	}

	return rdb
}

// Unmarshals the cached data into the given arbitrary struct
func unmarshalCacheData(data string, v interface{}) error {
	err := json.Unmarshal([]byte(data), v)
	if err != nil {
		return fmt.Errorf("error decoding JSON data: %v", err)
	}
	return nil
}
