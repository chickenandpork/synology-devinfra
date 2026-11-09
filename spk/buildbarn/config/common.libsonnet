{
  blobstore: {
    contentAddressableStorage: {
      sharding: {
        shards: {
          "0": {
            backend: { grpc: { client: { address: "storage:8981" } } },
            weight: 1,
          },
        },
      },
    },
    actionCache: {
      completenessChecking: {
        backend: {
          sharding: {
            shards: {
              "0": {
                backend: { grpc: { client: { address: "storage:8981" } } },
                weight: 1,
              },
            },
          },
        },
        maximumTotalTreeSizeBytes: 64 * 1024 * 1024,
      },
    },
  },
  fileSystemAccessCache: {
    sharding: {
      shards: {
        "0": {
          backend: { grpc: { client: { address: "storage:8981" } } },
          weight: 1,
        },
      },
    },
  },
  browserUrl: "http://localhost:8980/browser",
  maximumMessageSizeBytes: 16 * 1024 * 1024,
  global: {
    diagnosticsHttpServer: {
      httpServers: [{
        listenAddresses: [":9980"],
        authenticationPolicy: { allow: {} },
      }],
      enablePrometheus: true,
      enablePprof: true,
      enableActiveSpans: true,
    },
  },
}
