# CELLS Quick Start for Game Integration

## ✅ Fixed Issues

1. ✅ API server now defaults to HTTP port 8080 (was 8443 HTTPS)
2. ✅ TLS disabled by default for development
3. ✅ Authentication works without CGO (HMAC fallback)
4. ✅ Health endpoint accessible

## Start CELLS

```bash
cd CELLS
./CELLS.exe
```

The API will be available at: **http://localhost:8080/api/v1**

## Test API

```bash
# Health check
curl http://localhost:8080/api/v1/health

# Expected response:
# {"status":"healthy","timestamp":1234567890,"version":"1.0.0"}
```

## Configuration

The config file `CELLS/CELLS.yaml` is already created with:
- Port: 8080
- TLS: Disabled
- Storage: File-based (no SQLite needed)

## Game Integration

Your game server should connect to:
```
http://localhost:8080/api/v1
```

Update `Greed-Island/server/.env`:
```bash
CELLS_API_URL=http://localhost:8080/api/v1
```

## API Endpoints

| Endpoint | Method | Auth | Description |
|----------|--------|------|-------------|
| `/api/v1/health` | GET | No | Health check |
| `/api/v1/auth/login` | POST | No | Login |
| `/api/v1/auth/register` | POST | No | Register |
| `/api/v1/wallet/create` | any | No | Removed (410 Gone): create the wallet in CELLS Hive or with `CELLScli wallet new` |
| `/api/v1/wallet/balance` | GET | Yes | Get balance |
| `/api/v1/wallet/send` | POST | Yes | Send $JOLLY |
| `/api/v1/wallet/address` | GET | Yes | Get address |
| `/api/v1/transactions` | GET | Yes | Transaction history |

## Status

✅ CELLS running
✅ API on port 8080
✅ Ready for game integration

