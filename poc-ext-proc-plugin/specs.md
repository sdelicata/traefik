# Spécifications des fonctionnalités critiques ext-proc (P0)

## Vue d'ensemble

Ce document décrit les fonctionnalités critiques manquantes dans l'implémentation ext-proc de Traefik par rapport à la spécification complète d'Envoy. Ces fonctionnalités sont classées comme **priorité P0** car elles sont essentielles pour une implémentation complète du protocole ext-proc.

---

## 1. Support des Trailers HTTP

### 🎯 Fonctionnement attendu

Les trailers HTTP permettent d'ajouter des headers après le corps du message HTTP. Dans le contexte ext-proc, ils doivent être supportés pour :

- **Request Trailers** : Headers envoyés après le body de la requête
- **Response Trailers** : Headers envoyés après le body de la réponse
- **Processing Flow** : Les trailers sont traités après le body, avant la fin du stream

**Séquence attendue :**
```
Request: Headers → Body → Trailers
Response: Headers → Body → Trailers
```

### ✅ Critères d'acceptance

- [ ] Support des `ProcessingRequest_RequestTrailers`
- [ ] Support des `ProcessingRequest_ResponseTrailers` 
- [ ] Gestion des trailers dans le flow de traitement
- [ ] Mutations des trailers (ajout/suppression/modification)
- [ ] Préservation des trailers existants si non modifiés
- [ ] Validation des noms de trailers (conformité HTTP)
- [ ] Tests unitaires couvrant tous les cas de figure
- [ ] Tests d'intégration avec serveur ext-proc réel

### 🧪 Tests d'intégration à ajouter à validate.sh

```bash
# Test 10: Request Trailers Processing
log_info "Test 10: Request Trailers Processing"
run_test "Request with trailers processed correctly" \
    "curl -s -H 'Host: $TEST_HOST' -H 'X-Enable-Trailers: true' --data-raw 'test body' $TRAEFIK_URL/trailers | grep -q 'X-Processed-Trailer:'" \
    "Should add processed trailer to response"

# Test 11: Response Trailers Processing  
log_info "Test 11: Response Trailers Processing"
run_test "Response trailers are processed" \
    "curl -s -H 'Host: $TEST_HOST' -H 'X-Request-Trailer: value' $TRAEFIK_URL/response-trailers -D- | grep -q 'x-response-trailer:'" \
    "Should process response trailers"

# Test 12: Trailers Mutation
log_info "Test 12: Trailers Mutation"
run_test "Trailers can be mutated by ext-proc" \
    "curl -s -H 'Host: $TEST_HOST' --data-raw 'modify-trailers' $TRAEFIK_URL/ -D- | grep -q 'x-modified-trailer:'" \
    "Should modify trailers based on ext-proc response"
```

### 📚 Références

**Spec Protocol:**
- [ProcessingRequest Trailers](https://www.envoyproxy.io/docs/envoy/latest/api-v3/service/ext_proc/v3/external_processor.proto#envoy-v3-api-msg-service-ext-proc-v3-processingrequest)
- [HttpTrailers Definition](https://www.envoyproxy.io/docs/envoy/latest/api-v3/service/ext_proc/v3/external_processor.proto#envoy-v3-api-msg-service-ext-proc-v3-httptrailers)

**Envoy Implementation:**
- [Trailers Processing in ext_proc.cc](https://github.com/envoyproxy/envoy/blob/main/source/extensions/filters/http/ext_proc/ext_proc.cc#L400-L450)
- [Trailer State Management](https://github.com/envoyproxy/envoy/blob/main/source/extensions/filters/http/ext_proc/processor_state.cc#L200-L250)

---

## 2. Response Body Processing

### 🎯 Fonctionnement attendu

Le traitement des corps de réponses HTTP permet de :

- **Inspection** : Analyser le contenu des réponses avant envoi au client
- **Mutation** : Modifier, remplacer ou enrichir le body de réponse
- **Validation** : Vérifier la conformité des réponses
- **Logging/Audit** : Enregistrer les réponses pour audit

**Modes supportés :**
- `BUFFERED` : Buffer complet avant traitement
- `STREAMED` : Traitement chunk par chunk  
- `BUFFERED_PARTIAL` : Buffer partiel avec seuils

### ✅ Critères d'acceptance

- [ ] Support des `ProcessingRequest_ResponseBody`
- [ ] Support des `ProcessingResponse_ResponseBody`
- [ ] Implémentation du mode `BUFFERED` pour response body
- [ ] Implémentation du mode `STREAMED` pour response body
- [ ] Gestion des mutations de body (remplacement/modification)
- [ ] Préservation des headers Content-Length/Transfer-Encoding
- [ ] Gestion des erreurs de traitement (fail-safe)
- [ ] Support des différents Content-Types
- [ ] Streaming bidirectionnel pour gros bodies
- [ ] Tests de performance avec gros payloads

### 🧪 Tests d'intégration à ajouter à validate.sh

```bash
# Test 13: Response Body Inspection
log_info "Test 13: Response Body Inspection"  
run_test "Response body is inspected by ext-proc" \
    "curl -s -H 'Host: $TEST_HOST' -H 'X-Inspect-Response: true' $TRAEFIK_URL/large-response | grep -q 'X-Body-Processed:'" \
    "Should add header indicating body was processed"

# Test 14: Response Body Mutation
log_info "Test 14: Response Body Mutation"
run_test "Response body can be modified" \
    "curl -s -H 'Host: $TEST_HOST' -H 'X-Modify-Response: upper' $TRAEFIK_URL/text-response | grep -q 'MODIFIED BY EXT-PROC'" \
    "Should modify response body content"

# Test 15: Large Response Body Handling
log_info "Test 15: Large Response Body Handling"  
run_test "Large response bodies are handled correctly" \
    "curl -s -H 'Host: $TEST_HOST' $TRAEFIK_URL/large-json -w '%{size_download}' -o /dev/null | awk '{print (\$1 > 1000)}' | grep -q '1'" \
    "Should handle large response bodies without corruption"

# Test 16: Response Body Error Handling
log_info "Test 16: Response Body Error Handling"
run_test "Response processing errors are handled gracefully" \
    "curl -s -H 'Host: $TEST_HOST' -H 'X-Trigger-Processing-Error: true' $TRAEFIK_URL/ -w '%{http_code}' -o /dev/null | grep -q '200'" \
    "Should continue normally when response processing fails"
```

### 📚 Références

**Spec Protocol:**
- [BodyResponse Definition](https://www.envoyproxy.io/docs/envoy/latest/api-v3/service/ext_proc/v3/external_processor.proto#envoy-v3-api-msg-service-ext-proc-v3-bodyresponse)
- [HttpBody Processing](https://www.envoyproxy.io/docs/envoy/latest/api-v3/service/ext_proc/v3/external_processor.proto#envoy-v3-api-msg-service-ext-proc-v3-httpbody)

**Envoy Implementation:**
- [Response Body Processing](https://github.com/envoyproxy/envoy/blob/main/source/extensions/filters/http/ext_proc/ext_proc.cc#L500-L600)
- [Body Buffer Management](https://github.com/envoyproxy/envoy/blob/main/source/extensions/filters/http/ext_proc/processor_state.cc#L300-L400)

---

## 3. ImmediateResponse complète

### 🎯 Fonctionnement attendu

L'ImmediateResponse permet au serveur ext-proc de court-circuiter le traitement normal et de retourner une réponse immédiate avec :

- **Status codes personnalisés** : 200, 400, 403, 500, etc.
- **Headers personnalisés** : Content-Type, Cache-Control, etc.  
- **Body personnalisé** : Message d'erreur, contenu JSON, HTML
- **Grpc status** : Codes d'erreur gRPC si applicable

**Cas d'usage :**
- Authentification/Autorisation
- Rate limiting  
- Validation de requêtes
- Réponses d'erreur enrichies

### ✅ Critères d'acceptance

- [ ] Support complet de `ImmediateResponse` avec tous les champs
- [ ] Gestion des headers personnalisés dans ImmediateResponse
- [ ] Support des status codes HTTP complets (1xx-5xx)
- [ ] Gestion du body personnalisé avec bon Content-Type
- [ ] Bypass complet du pipeline downstream
- [ ] Préservation des headers de traçabilité
- [ ] Support des redirections (3xx) avec Location header
- [ ] Gestion des cookies dans les réponses immédiates
- [ ] Tests avec différents Content-Types (JSON, HTML, Text)
- [ ] Validation des performances (latence minimale)

### 🧪 Tests d'intégration à ajouter à validate.sh

```bash
# Test 17: ImmediateResponse with Custom Status
log_info "Test 17: ImmediateResponse with Custom Status"
run_test "ext-proc can return custom status codes" \
    "curl -s -H 'Host: $TEST_HOST' -H 'X-Return-Status: 418' $TRAEFIK_URL/teapot -w '%{http_code}' -o /dev/null | grep -q '418'" \
    "Should return custom 418 status code"

# Test 18: ImmediateResponse with Custom Headers
log_info "Test 18: ImmediateResponse with Custom Headers"
run_test "ImmediateResponse includes custom headers" \
    "curl -s -H 'Host: $TEST_HOST' -H 'X-Add-Custom-Header: true' -I $TRAEFIK_URL/immediate | grep -q 'X-Custom-Response:'" \
    "Should include custom headers in immediate response"

# Test 19: ImmediateResponse with JSON Body
log_info "Test 19: ImmediateResponse with JSON Body"
run_test "ImmediateResponse can return JSON content" \
    "curl -s -H 'Host: $TEST_HOST' -H 'X-Return-JSON: true' $TRAEFIK_URL/api | jq -r '.message' | grep -q 'processed'" \
    "Should return valid JSON in immediate response"

# Test 20: ImmediateResponse Redirect
log_info "Test 20: ImmediateResponse Redirect"  
run_test "ext-proc can send redirects" \
    "curl -s -H 'Host: $TEST_HOST' -H 'X-Redirect-To: /new-location' $TRAEFIK_URL/redirect -w '%{http_code}' -o /dev/null | grep -q '302'" \
    "Should return 302 redirect response"

# Test 21: ImmediateResponse Performance
log_info "Test 21: ImmediateResponse Performance"
run_test "ImmediateResponse has low latency" \
    "curl -s -H 'Host: $TEST_HOST' -H 'X-Immediate-Response: true' $TRAEFIK_URL/fast -w '%{time_total}' -o /dev/null | awk '{print (\$1 < 0.1)}' | grep -q '1'" \
    "Should respond quickly for immediate responses"
```

### 📚 Références

**Spec Protocol:**
- [ImmediateResponse Definition](https://www.envoyproxy.io/docs/envoy/latest/api-v3/service/ext_proc/v3/external_processor.proto#envoy-v3-api-msg-service-ext-proc-v3-immediateresponse)
- [CommonResponse Status Codes](https://www.envoyproxy.io/docs/envoy/latest/api-v3/service/ext_proc/v3/external_processor.proto#envoy-v3-api-enum-service-ext-proc-v3-commonresponse-responsestatus)

**Envoy Implementation:**
- [ImmediateResponse Handling](https://github.com/envoyproxy/envoy/blob/main/source/extensions/filters/http/ext_proc/ext_proc.cc#L250-L300)
- [Local Reply Generation](https://github.com/envoyproxy/envoy/blob/main/source/extensions/filters/http/ext_proc/processor_state.cc#L100-L150)

---

## 4. Attributes Context

### 🎯 Fonctionnement attendu

Les Attributes Context permettent l'échange de métadonnées structurées entre Traefik et le serveur ext-proc :

- **Request Attributes** : Métadonnées sur la requête (IP client, certificats TLS, etc.)
- **Dynamic Metadata** : Enrichissement du contexte par le serveur ext-proc
- **State Sharing** : Partage d'état entre différentes phases de traitement
- **Observability** : Données pour monitoring et debugging

**Types de métadonnées :**
- Informations réseau (IP, port, protocole)
- Contexte TLS (certificats, cipher suites)
- Headers spéciaux (User-Agent parsing, etc.)
- État applicatif personnalisé

### ✅ Critères d'acceptance

- [ ] Support des `attributes` dans ProcessingRequest
- [ ] Support des `dynamic_metadata` dans CommonResponse
- [ ] Sérialisation/Désérialisation des attributs Protobuf
- [ ] Injection d'attributs système (IP, TLS info, etc.)  
- [ ] Enrichissement par attributs personnalisés
- [ ] Persistance des attributs entre phases de traitement
- [ ] Validation des types d'attributs
- [ ] Documentation des attributs disponibles
- [ ] Tests avec différents types d'attributs
- [ ] Benchmarks de performance avec gros contextes

### 🧪 Tests d'intégration à ajouter à validate.sh

```bash
# Test 22: Basic Attributes Context  
log_info "Test 22: Basic Attributes Context"
run_test "Request attributes are sent to ext-proc" \
    "curl -s -H 'Host: $TEST_HOST' -H 'X-Check-Attributes: true' $TRAEFIK_URL/attrs | grep -q 'client-ip:'" \
    "Should include client IP in attributes"

# Test 23: TLS Attributes
log_info "Test 23: TLS Attributes"
run_test "TLS attributes are included when available" \
    "curl -s -k -H 'Host: $TEST_HOST' -H 'X-Check-TLS: true' https://localhost/tls-attrs | grep -q 'tls-version:'" \
    "Should include TLS information in attributes"

# Test 24: Dynamic Metadata Injection
log_info "Test 24: Dynamic Metadata Injection" 
run_test "ext-proc can inject dynamic metadata" \
    "curl -s -H 'Host: $TEST_HOST' -H 'X-Add-Metadata: user-id=123' $TRAEFIK_URL/metadata | grep -q 'X-User-Context:'" \
    "Should use injected metadata in subsequent processing"

# Test 25: Attribute Persistence  
log_info "Test 25: Attribute Persistence"
run_test "Attributes persist across processing phases" \
    "curl -s -H 'Host: $TEST_HOST' -H 'X-Set-Context: phase1' -X POST --data 'test' $TRAEFIK_URL/multi-phase | grep -q 'X-Context-From-Phase1:'" \
    "Should maintain context from headers to body processing"

# Test 26: Custom Attributes
log_info "Test 26: Custom Attributes"
run_test "Custom attributes can be defined and used" \
    "curl -s -H 'Host: $TEST_HOST' -H 'X-Custom-Attr: special-value' $TRAEFIK_URL/custom | grep -q 'processed-special-value'" \
    "Should process custom attribute values"
```

### 📚 Références

**Spec Protocol:**
- [ProcessingRequest Attributes](https://www.envoyproxy.io/docs/envoy/latest/api-v3/service/ext_proc/v3/external_processor.proto#envoy-v3-api-field-service-ext-proc-v3-processingrequest-attributes)
- [Dynamic Metadata](https://www.envoyproxy.io/docs/envoy/latest/api-v3/config/core/v3/base.proto#envoy-v3-api-msg-config-core-v3-metadata)

**Envoy Implementation:**
- [Attributes Collection](https://github.com/envoyproxy/envoy/blob/main/source/extensions/filters/http/ext_proc/ext_proc.cc#L150-L200)
- [Dynamic Metadata Handling](https://github.com/envoyproxy/envoy/blob/main/source/extensions/filters/http/ext_proc/processor_state.cc#L50-L100)

---

## 🚀 Plan de développement

### Phase 1: Trailers Support (Semaine 1)
- Implémentation des structures de données
- Logique de traitement des trailers
- Tests unitaires et d'intégration

### Phase 2: Response Body Processing (Semaine 2)  
- Mode BUFFERED pour response body
- Mutations de body de réponse
- Gestion des erreurs et fail-safe

### Phase 3: ImmediateResponse complète (Semaine 3)
- Headers et body personnalisés
- Support de tous les status codes
- Tests de performance

### Phase 4: Attributes Context (Semaine 4)
- Collecte d'attributs système
- Dynamic metadata injection
- Tests avec métadonnées complexes

### Phase 5: Integration & Testing (Semaine 5)
- Tests de régression complets
- Documentation utilisateur
- Optimisations de performance

---

## 📋 Checklist de validation globale

- [ ] Tous les tests unitaires passent
- [ ] Tous les tests d'intégration passent  
- [ ] Performance acceptable (< 10ms latency overhead)
- [ ] Consommation mémoire raisonnable (< 50MB par connexion)
- [ ] Documentation complète
- [ ] Exemples de configuration
- [ ] Migration guide depuis version actuelle
- [ ] Compatibilité ascendante préservée

---

## 📖 Ressources additionnelles

- [Envoy External Processing Filter](https://www.envoyproxy.io/docs/envoy/latest/configuration/http/http_filters/ext_proc_filter)
- [gRPC Streaming Best Practices](https://grpc.io/docs/guides/streaming/)
- [HTTP Trailer Specification (RFC 7230)](https://tools.ietf.org/html/rfc7230#section-4.1.2)
- [Traefik Middleware Development Guide](https://doc.traefik.io/traefik/plugins/middleware/)