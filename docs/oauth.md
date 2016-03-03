# OAuth

## Obtaining Access Token (Login)

### Resource Owner Password Credentials Grant

```
POST /oauth/token
```

**POST Form Params**

| Key         | Type   | Required? | Description        |
| ----------- | ------ | --------- | ------------------ |
| grant\_type | string | Required  | Must be `password` |
| username    | string | Required  | user's email       |
| password    | string | Require   | user's password    |

**Possible responses**

* **200** - Token issued
  Example:
  ```json
  {
    "access_token": "2YotnFZFEjr1zCsicMWpAA",
    "token_type": "bearer",
    "client_id": "73c24fbc"
  }
  ```

* **400** - Invalid params
  Example:
  ```json
  {
    "error": "invalid_grant",
    "error_description": "user credentials are invalid"
  }
  ```

  ```json
  {
    "error": "invalid_grant",
    "error_description": "user has not confirmed email address"
  }
  ```

* **401** - Invalid Authorize header
  Example:
  ```json
  {
    "error": "invalid_client",
    "error_description": "client credentials are invalid"
  }
  ```

## Invalidating Access Token (Logout)

```
DELETE /oauth/token
```

**Headers**

| Key           | Value        | Description               |
| ------------- | ------------ | ------------------------- |
| Authorization | Bearer TOKEN | TOKEN is the access token |

**Possible responses**

* **200** - Token invalidated
  ```json
  {
    "invalidated": true
  }
  ```

* **401** - Invalid token
  ```json
  {
    "invalidated": false,
    "error": "invalid_token",
    "error_description": "access token is invalid"
  }
  ```
