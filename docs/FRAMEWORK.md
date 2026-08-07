# 프레임워크화 검토

`go-app`을 "복사해서 시작하는 템플릿"에서 "따르면 되는 프레임워크"로 옮기기 위한
검토. 무엇을 `runtime` 패키지로 흡수할 수 있는지, 무엇을 CLI가 대신 해줄 수 있는지,
무엇을 강제하는 것이 이득인지를 각각 근거와 비용과 함께 정리한다.

이 문서는 작업에 대한 것이지 앱에 대한 것이 아니다. `PLAN.md`와 같은 성격이다.

## 0. 지금 경계가 어디에 있는가

프레임워크는 이미 네 겹으로 나뉘어 있다. 새로 만드는 것이 아니라 **다섯 번째 겹이
비어 있는 것**이다.

| 겹 | 무엇 | 사는 곳 |
| --- | --- | --- |
| 계약 | 엔티티 선언, `orm.field`, `orm.message` | `protobuf-orm` (원격 레지스트리) |
| 생성기 | 서비스 계약, 메시지, ent 스키마, CRUD 서버 | `protoc-gen-orm-*` |
| 런타임(생성물용) | `enttx`, `entpatch`, `entpage` | `protoc-gen-orm-ent/runtime` |
| 런타임(횡단) | 로깅/추적(`otx`), CLI(`xli`), 설정 확장(`mkot`), 유틸(`z`) | 각 저장소 |
| **런타임(앱)** | **`grpcx`, `config`, `migrate`, `frame`, `auth`, `ox`, `id`** | **없음 — 매 앱이 복사** |

다섯 번째가 지금 `internal/`과 `cmd/config/`와 `server/`에 손으로 쓰여 있고, 앱을
만들 때마다 통째로 복사된다. 복사된 것은 갈라지고, 갈라진 것은 한쪽에서 고친 버그가
다른 쪽에 남는다 — `PLAN.md`가 `server/gate`의 열세 개 오버라이드에 대해 이미 한 번
겪은 이야기다.

정리하면 프레임워크화의 본체는 **다섯 번째 겹을 별도 모듈로 떼는 것**이고, ID 규칙은
그 첫 번째 입주자다.

---

## 1. 시작점: ID

### 1.1 지금 어떤가

ID는 proto에서 `bytes id = 1 [(orm.field) = {type: TYPE_UUID, key: true, default: ""}]`
이고, ent에서 `uuid.UUID`, 와이어에서 16바이트다. 그리고 실제 생성은 **생성된 코드
안에 박혀 있다**:

```go
// server/bare/holder.g.go
if req.HasId() {
    if v, err := uuid.FromBytes(req.GetId()); err != nil {
        return nil, status.Errorf(codes.InvalidArgument, "id: %s", err)
    } else {
        q.SetID(v)
    }
} else {
    q.SetID(uuid.New())   // v4
}
```

여기서 나오는 사실 세 가지:

- **v4다.** 시간 정렬이 없다. 커서 페이징은 `date_created`를 앞세우고 키를 tiebreaker로
  쓰므로 정확성에는 문제가 없지만, 기본키 인덱스의 지역성은 얻지 못하고 있다.
- **요청이 ID를 정할 수 있다.** `req.HasId()`면 그대로 쓴다. 검사는 `uuid.FromBytes`의
  "16바이트인가"와 `core.CheckId`의 "nil이 아닌가" 둘뿐이다.
- **앱이 끼어들 자리가 없다.** ID를 다르게 만들고 싶으면 엔티티마다 `core`에서 `Add`를
  오버라이드해서 미리 채워 넣는 수밖에 없다. 그것은 `PLAN.md`가 감사 로그와 테넌트 벽에
  대해 각각 "오버라이드 목록이 아니라 수렴점을 찾아라"라고 결론 낸 바로 그 모양이다.

### 1.2 `hid` 형식이 여기서 정확히 무엇을 해결하는가

`hid`는 UUIDv7 위에 version을 8로 바꾸고 **바이트 9에 도메인 1바이트**를 심는다. 즉
식별자가 자기가 무엇에 대한 것인지를 스스로 말한다.

이 저장소에서 그것이 사는 자리는 추상적이지 않다. `proto/go_app/audit.proto`와
README가 **이미 그 부재를 비용으로 적어 두었다**:

> Which kind of thing it was is not stored, because an identifier is unique across
> every one of them (...) The cost is that a row erased later leaves an identifier
> nothing answers to, and **no way to say what it used to be**.

도메인 바이트는 이 비용을 정확히 없앤다. 행이 지워져도 `Id.Domain()`이 "그것은 Holder
였다"라고 말한다. `PLAN.md` Phase 3이 `object_tenant_id`를 두고 "생성기 전체를 건드려야
하고 Erase에서는 어차피 못 읽는다"라며 포기한 문제와 대비된다 — 이쪽은 **읽을 필요가
없다. 식별자 자체가 들고 있다.**

부수적으로 따라오는 것들:

- **엣지에서의 타입 오류 검출.** Holder의 ID를 `TenantRef`에 넣으면 지금은 쿼리까지
  가서 `NotFound`가 된다. 도메인 바이트가 있으면 DB를 건드리기 전에
  `InvalidArgument`이고, 오류 메시지가 "이건 holder다"라고 말할 수 있다.
- **`AuditService.List`의 다형 조회.** `object_id`로 감사 로그를 읽은 다음 그 객체를
  보여주려면 지금은 모든 테이블을 찔러 봐야 한다. 도메인으로 바로 서비스를 고른다.
- **v7 기반의 시간 정렬.** 기본키 삽입이 append-only에 가까워진다.

### 1.3 그런데 어디에 꽂을 것인가 — 세 번째 수렴점

`PLAN.md`의 뼈대는 이렇게 읽힌다.

| | 수렴점 | 상태 |
| --- | --- | --- |
| 모든 쓰기가 자기를 보고한다 | `bare.Recorder` | 완료 |
| 모든 읽기가 술어를 싣는다 | `bare.Scope` | 완료 |
| **모든 행이 키를 얻는다** | **없음** | **이 문서** |

그러니까 필요한 것은 `hid` 패키지 자체가 아니라 **`Store`에 `Rec`, `Scope` 옆에 놓이는
세 번째 훅**이다. 이름은 `Minter`가 적당하다.

```go
// 생성기(protoc-gen-orm-ent) 쪽
type Store struct {
    Db    *ent.Client
    Rec   Recorder
    Scope Scope
    Mint  Minter   // 새로
}

// Minter는 엔티티마다 하나씩, Scope와 같은 모양으로.
// 인터페이스에 메서드를 두는 이유는 todo.md #7의 결론과 같다.
type Minter interface {
    HolderMint(ctx context.Context, given uuid.UUID, ok bool) (uuid.UUID, error)
    TenantMint(...) ...
    AuditMint(...)  ...
}
```

생성된 `Add`는 이렇게 바뀐다:

```go
k, err := mint(ctx, s.Mint, req.GetId())   // nil이면 uuid.New()로 폴백
if err != nil {
    return nil, err
}
q.SetID(k)
```

**생성기는 도메인이 무엇인지 배우지 않는다.** 훅을 부르고, 무엇을 돌려줄지는 앱이
말한다 — `Recorder`가 감사에 대해 아무것도 모르고 `Scope`가 테넌트에 대해 아무것도
모르는 것과 같은 규율이다. 앱 쪽 구현은 이렇게 된다:

```go
// runtime/id 쪽 — 엔티티를 모른다
func Mint(d Domain, given uuid.UUID, ok bool) (uuid.UUID, error) {
    if !ok {
        return New(d), nil
    }
    if Of(given) != d {
        return uuid.Nil, status.Errorf(codes.InvalidArgument,
            "id: names a %s, and this is a %s", Of(given), d)
    }
    return given, nil
}

// 앱 쪽 — 생성될 수 있다 (§4 참고)
func (m minter) HolderMint(ctx context.Context, g uuid.UUID, ok bool) (uuid.UUID, error) {
    return id.Mint(id.Holder, g, ok)
}
```

이 모양의 값어치는 `core.CheckId`와 `core.NobodyId`가 사라진다는 데 있다. "nil은 아무도
아니므로 아무도 가질 수 없다"는 규칙은 "도메인이 맞지 않는 ID는 거절한다"의 특수한
경우가 된다 — nil의 도메인은 `Unknown`이고, `Unknown`인 엔티티는 없다.

### 1.4 두 단계로 나눠서 도입할 것

`hid.Id`를 곧바로 ent/proto의 타입으로 만드는 것은 **생성기 변경 없이는 불가능**하다:

- `field.UUID(name string, typ driver.Valuer)`는 `driver.Valuer`를 요구한다. `hid.Id`는
  `Value()`도 `Scan()`도 `MarshalText()`도 없다.
- `protoc-gen-orm-ent`가 `TYPE_UUID`를 `uuid.UUID`로 **하드코딩**해서 찍는다.
  `HolderGetKey`, `where` 절, `mutation.go`까지 전부.

그래서:

**Phase A — 타입은 그대로, 규칙만 도입 (생성기 변경: `Minter` 훅 하나)**

- 저장/전송 타입은 계속 `uuid.UUID` / `bytes`.
- `runtime/id`가 도메인 바이트를 심고 검사한다. `id.New(d)`, `id.Of(v)`, `id.Mint(...)`.
- 얻는 것: 도메인 검증, 감사 로그의 "무엇이었는가", v7 정렬, 다형 조회. 즉 §1.2의
  거의 전부.
- 잃는 것: 컴파일 타임 타입 안전성. `HolderId`와 `TenantId`는 여전히 같은 `uuid.UUID`다.

**Phase B — 타입까지 (생성기 변경: 키 타입을 설정 가능하게)**

- `buf.gen.yaml`에 `key_type: github.com/lesomnus/go-app/runtime/id.Id` 같은 옵션.
- `id.Id`가 `driver.Valuer`, `sql.Scanner`, `encoding.TextMarshaler`를 구현.
- 얻는 것: 잘못된 ID를 넘기는 것이 컴파일 오류.
- 비용: 생성기 전반. 그리고 엔티티별 타입(`HolderId`)까지 가려면 생성기가 키 타입을
  엔티티마다 다르게 찍어야 하는데, 그러면 `Change.Key any`나 `frame`의 시그니처가 전부
  제네릭이 된다.

**Phase A만 해도 실질 이득의 대부분을 가져온다.** B는 나중에 판단할 것.

### 1.5 `hid` 자체에 대해 — 그대로 베끼면 안 되는 세 가지

**하나. `v[6] = 0b1000_0000`은 v7의 단조 카운터를 깬다.**

`google/uuid`의 `NewV7`은 같은 밀리초 안에서의 순서를 위해 바이트 6의 하위 4비트와
바이트 7에 걸친 **12비트 시퀀스 카운터**를 쓴다. `hid.New`는 바이트 6을 통째로
덮어써서 그중 상위 4비트를 날린다. 남는 것은 바이트 7의 8비트뿐이고, 같은 밀리초에
256개를 넘기면 카운터가 되감기면서 정렬이 무너진다.

version만 바꾸고 카운터는 남기면 된다:

```go
v[6] = 0x80 | (v[6] & 0x0F)   // version=8, rand_a(=seq)의 상위 4비트 보존
```

**둘. `Parse`/`From`이 아무것도 검증하지 않는다.**

16바이트면 무엇이든 `Id`가 된다. v4 UUID를 넣으면 바이트 9가 우연히 `3`이라서
"Holder"라고 주장한다. 도메인을 믿을 수 있으려면 파싱에서 version과 variant를 확인해야
한다. 이것이 §1.3에서 `Mint`가 **요청이 준 ID의 도메인을 반드시 검사해야 하는** 이유이기도
하다 — 검사하지 않으면 호출자가 도메인을 마음대로 주장하고, 그러면 감사 로그의
"무엇이었는가"가 호출자의 말이 된다.

**셋. `Domain` 열거형이 손으로 쓰인 switch 두 개다.**

`domain.go`의 `String()`과 `DomainString()`은 엔티티를 추가할 때마다 세 곳(상수,
switch 두 개)을 고쳐야 하고, 하나를 빠뜨려도 컴파일된다. 프레임워크에서는 **proto에서
생성되어야 한다**:

```proto
option (orm.message) = {
  rpc: {crud: true}
  domain: 3          // 새로. 고정되고 재사용되지 않는 번호.
};
```

번호는 필드 번호와 같은 성격이다 — 한 번 정하면 바꾸지 않고, 지운 것은 `reserved`로
남긴다. 생성기가 상수와 `String()`과 `Minter` 구현을 함께 찍는다. 손으로 쓰는 것은
proto의 숫자 하나뿐.

---

## 2. `runtime`으로 흡수 가능한 것

세 등급으로 나뉜다. 나누는 기준은 **앱의 생성된 타입(`go_app.Server`, `go_app.Holder`)을
이름으로 부르는가**이다.

### Tier 1 — 앱 타입을 전혀 모른다. 지금 당장 옮길 수 있다.

| 지금 | 옮길 곳 | 규모 | 비고 |
| --- | --- | --- | --- |
| `internal/grpcx/*` | `runtime/grpcx` | ~900줄 | `Recover`, `Deadline`, `Log`, `Otel`, `Seed`, `Closed`, `Limit`, `Limiter`, `MemLimiter`, `StreamWithContext`. 앱 이름조차 안 나온다. **가장 순수한 후보.** |
| `internal/migrate/*` | `runtime/migrate` | ~300줄 | atlas `Plan`/`Apply`/`Pending`/`revision`. `Dialect` 상수만 앱이 정한다. |
| `cmd/config/env.go` | `runtime/config` | ~230줄 | 리플렉션 기반 환경변수 바인딩. `EnvPrefix`만 인자로. **`EnvNames`가 이미 있어서 CLI가 바로 쓸 수 있다(§3).** |
| `cmd/config/{config,server,tls,otel,db,db-*}.go` | `runtime/config` | ~600줄 | 파일 읽기 + `mkot.ExpandEnv` + `Evaluate` + 드라이버 레지스트리 + TLS/keepalive/limit. `Config` 구조체만 앱이 조립한다. |
| `cmd/version/*` + `scripts/gen-version-file.sh` | `runtime/version` | ~40줄 | `debug.ReadBuildInfo`와 `-ldflags`로 대체하면 스크립트 자체가 사라진다. |
| `server/core/alias.go` | `runtime/slug` | ~35줄 | 정규화 + 검증. oasys의 `slug` 패키지와 같은 자리. |
| (신규) | `runtime/id` | — | §1. |

Tier 1만 옮겨도 `internal/`이 거의 비고, 앱 저장소에서 손으로 유지할 코드가 대략
**2,000줄 줄어든다.**

### Tier 2 — 앱 타입을 안다. 제네릭이나 생성기가 필요하다.

| 지금 | 문제 | 답 |
| --- | --- | --- |
| `server/frame/*` | `Frame.Actor`가 `*go_app.Holder` | `Frame[A Actor]` 제네릭. `Actor`는 `Id() []byte; TenantId() []byte` 두 메서드짜리 인터페이스. `Grant`와 `Tenants`는 **이미 앱 타입을 모른다** — 통째로 Tier 1이다. |
| `server/auth/*` | `Identity.Ref`가 `*go_app.HolderRef`, `Resolver`가 `go_app.Server` | `Handler`/`Seq`/`Plain`/`MTLS`/`Bearer`/`Interceptor`는 클레임을 **문자열 쌍**(tenant, holder)으로 다루면 앱을 모른다. `Resolver`만 앱이 구현한다 — 지금의 `ServerResolver` 다섯 줄. |
| `server/audit/recorder.go` | `go_app.AuditAddRequest`를 만든다 | 프레임워크는 `Change` → 필드 묶음까지만 만들고, "그것으로 행을 쓰는" 한 함수를 앱이 준다. 또는 생성기가 `Audit` 엔티티를 인식해서 찍는다. |
| `internal/ox/*` | 스택 배선 전체 | 껍데기(`X`, memdb, bufconn, `Seed`로 테스트 로그 주입, `T()`)는 Tier 1. `NewServer`의 배선만 앱에 남는다 — 그리고 남아야 한다(§5). |
| `server/gate/{interceptor,limit,policy}.go` | `Policy`가 `*go_app.Holder`를 본다 | `Call{Actor A, Action string}` 제네릭. `ByTenant()`는 프레임에서 테넌트 ID만 꺼내므로 Tier 1에 가깝다. |

Tier 2의 공통 해법은 하나다: **프레임워크가 아는 것은 "행위자에게는 ID와 테넌트 ID가
있다"뿐이고, 나머지는 앱이 만족시키는 작은 인터페이스로 표현한다.** 그리고 그 인터페이스를
만족시키는 어댑터는 §4의 생성기가 찍는다.

### Tier 3 — 옮기면 안 되는 것

`README.md`가 자기 가치를 정확히 어디에 두고 있는지 읽으면 답이 나온다. 이 저장소의
자산은 코드가 아니라 **결정과 그 이유가 코드 옆에 쓰여 있다는 것**이다. 런타임 모듈로
옮기는 순간, 앱을 만든 사람은 그 이유를 읽지 않게 된다.

그러니 다음은 앱에 남는다:

- **`server/core`의 규칙들** — 별칭 정규화, `FilterLimit`, `Erase`가 Holder를 데려가는
  것. 도메인이다.
- **`server/gate`의 정책** — 벽 자체(`Wall()`)의 *모양*은 프레임워크가 줄 수 있지만,
  "테넌트가 벽이다"는 이 앱의 규칙이다.
- **`List`의 필터** — `PLAN.md`가 이미 결론 낸 것. 페이징만 런타임(`entpage`).
- **`cmd/serve.go`의 배선** — 여기가 가장 중요하다. 스택을 쌓는 열 줄, `sink`와
  `walled`를 따로 만드는 이유, 인터셉터의 순서. 이것을 `runtime.Serve(cfg)` 한 줄로
  감추면 프레임워크는 편해지지만, README가 두 페이지에 걸쳐 설명한 것들이 전부
  읽을 수 없는 곳으로 간다. **배선은 보이는 곳에 두고, 배선의 부품만 런타임이 준다.**

---

## 3. CLI가 대신할 수 있는 것

지금 `scripts/`에 셸 스크립트 다섯 개가 있고, 그중 둘(`gen-go.sh`, `init.sh`)은
`sed`로 소스를 고친다. 이것들이 CLI 후보다.

### 배포 방법

별도 설치가 필요 없다. `go.mod`에 이미 `tool` 지시자가 있으니:

```go
tool (
	github.com/lesomnus/go-app/cmd/goapp   // 추가
	...
)
```

```sh
$ go tool goapp gen
```

버전이 `go.mod`에 고정되고, devcontainer도 CI도 아무것도 더 설치하지 않는다.

### 명령

| 명령 | 대체하는 것 | 왜 CLI여야 하는가 |
| --- | --- | --- |
| `goapp gen` | `gen-service.sh` + `gen-go.sh` + `gen-ent.sh` | 세 단계의 **순서와 스테이징 디렉터리(`.gen`) 처리**가 지금 셸에 있다. 임포트 경로 재작성을 `sed`가 아니라 `go/ast`로 하면 문자열 우연 일치가 사라진다. |
| `goapp gen --check` | `ci.yaml`의 "생성하고 diff" 스텝 | 지금은 CI가 재생성 후 `git diff`로 본다. 명령이 스스로 판단하면 로컬에서도 같은 답이 나온다. |
| `goapp new <module>` | `init.sh` | `sed`로 5회 치환하는 대신 템플릿. 지금 방식은 `go_app`이라는 문자열이 우연히 들어간 주석까지 바꾼다. |
| `goapp entity add <Name>` | 없음 | **강제의 손잡이(§4).** proto 메시지 + `*.ext.proto` + `core` 스텁 + `Wall`의 `<E>Scope` 메서드 + `Minter`의 `<E>Mint` + 테스트를 한 번에 찍는다. |
| `goapp layer add <name>` | 없음 | 레이어마다 반드시 반복되는 것 — `Overlay` 임베드, `WithDriver`, `Build()`, `var _ go_app.Server`, `var _ enttx.Binder`. 네 줄짜리 규칙 두 개를 빠뜨리면 트랜잭션을 쓸 때에야 안다. |
| `goapp doctor` | 없음 | 정적 검사(§4의 표에서 "테스트/린트"로 표시된 것들). 모든 레이어가 `Binder`인가, 모든 엔티티에 `Scope`/`Mint`가 있는가, `*.g.go`가 최신인가, 마이그레이션이 스키마와 맞는가. |
| `goapp config env` | 없음 | `config.EnvNames`가 **이미 있다.** 모든 환경변수 이름을 출력한다. CI에서 문서와 대조하거나, `.env.example`을 생성하거나. |
| `goapp config schema` | 없음 | 설정의 JSON Schema를 내보내서 에디터가 `go-app.yaml`을 자동완성하게 한다. 리플렉션이 이미 있으니 거의 공짜. |
| `goapp dev` | `docker compose up` 수동 조합 | db 띄우기 → `migrate apply` → `serve --db-migrate` 를 한 명령으로. |

`migrate plan`/`apply`는 **앱 바이너리에 남겨야 한다.** ent 스키마를 링크해야 하고,
README가 강조하듯 배포는 "서빙할 바로 그 이미지로" 마이그레이션을 돌린다.

---

## 4. 강제할 것

프레임워크는 제약이다. 후보를 "무엇을 / 어떤 수단으로 / 무엇을 잃는가"로 적는다.
수단의 강도는 **컴파일 오류 > 생성기가 대신 씀 > 테스트 > 린트 > 문서** 순이다.

| # | 강제할 것 | 수단 | 비용 |
| --- | --- | --- | --- |
| 1 | 모든 ID는 도메인 태그된 UUID | `Minter` 훅(생성기) + `goapp entity add`가 `<E>Mint`를 찍음 | 생성기 변경 하나. §1.3 |
| 2 | 모든 엔티티에 `Scope` 메서드가 있다 | **`bare.Unscoped` 임베드를 없앤다** → 인터페이스 미구현 = 컴파일 오류 | 엔티티 추가가 빌드를 깬다. 하지만 지금은 **말없이 벽 밖으로 나간다** — `wall.go`의 주석이 "이게 거꾸로다"라고 스스로 적고 있고 `wall_test.go`로 때우고 있다. **fail-closed로 바꿀 것.** |
| 3 | 모든 레이어가 `enttx.Binder`다 | 이미 `var _ enttx.Binder[...]`로 컴파일 오류. `goapp layer add`가 찍고 `doctor`가 확인 | 없음. 이미 하고 있다. |
| 4 | 프레임 없는 요청은 거절된다 | 이미 그렇다. 우회는 `gate` 없는 서버 인스턴스 | 없음. `PLAN.md` Phase 7의 결론. |
| 5 | 일반 쓰기(`Patch`/`Apply`)는 기본으로 닫힌다 | 이미 `grpcx.Closed`, 설정 기본값 off | 없음. 런타임이 기본값을 소유하게만 하면 된다. |
| 6 | 검증은 Go로, 규칙 옆에 | 문서 + `entity add`가 자리를 만들어 줌 | 강제 불가. 하지만 자리를 만들어 주면 대부분 거기에 쓴다. |
| 7 | 레이어 순서 (`bare → core → audit → gate`) | `Builder`에 순위를 주고 `Build`가 역순을 거절 | **권하지 않는다.** `Build`의 값어치는 아무 미들웨어나 끼울 수 있다는 것이다. 순위를 넣으면 앱이 자기 레이어를 어디에 둘지 프레임워크에게 물어야 한다. `doctor`의 경고로 충분하다. |
| 8 | 헬스는 liveness/readiness 둘로 나뉜다 | 런타임이 두 이름을 소유. 앱은 readiness에 붙일 검사만 등록 | 없음. 지금 `cmd/health.go`가 손으로 쓰여 있다. |
| 9 | 데드라인/패닉복구/로그/추적은 항상 켜져 있다 | `runtime/grpcx.ServerOptions`가 기본. 빼려면 명시해야 함 | 없음. 이미 그 모양. |
| 10 | 요율 제한의 키는 테넌트다 | 런타임이 `ByTenant`를 기본으로 주되 교체 가능 | 없음. `PLAN.md` Phase 8. |
| 11 | 마이그레이션 디렉터리당 방언 하나 | 이미 `migrate.Dialect` + `speaks()` | 없음. |
| 12 | 설정 필드는 전부 환경변수 이름을 가진다 | 이미 리플렉션. `goapp config env`를 CI에 넣어 문서와 대조 | 없음. |

**2번이 이 표에서 실제로 무언가를 바꾸는 유일한 항목이다.** 나머지는 이미 하고 있거나
자리만 옮기는 것이다. 그리고 2번은 프레임워크가 템플릿과 다른 점을 정확히 보여준다 —
템플릿은 "이렇게 하세요"라고 주석에 쓰고, 프레임워크는 안 하면 빌드가 안 되게 한다.

---

## 5. 라이프사이클 (`todo.md` #11)

레이어가 백그라운드 루프를 갖고 싶다(GC 등). oasys는 `Server` 인터페이스에 `Spin(ctx)`을
올려 두고 재귀로 내려간다.

**이 저장소에서는 그렇게 하면 안 된다.** README가 자기 원칙을 이미 적었다:

> **A capability is found, not declared.** (...) reached with `Find` rather than by
> adding a method to `go_app.Server`. That is what keeps `Server` the generated set
> it is: extend it and every layer, every `Overlay` and every helper above has to be
> rewritten to match.

`Spin`을 `go_app.Server`에 올리면 생성기가 그것을 찍어야 하고, 모든 `Overlay`,
`StaticServer`, `UnimplementedServer`가 따라 바뀐다. 대신:

```go
// runtime/spin
type Spinner interface {
    Spin(ctx context.Context) error
}

// 스택을 걸으면서 Spin을 구현한 레이어만 띄운다.
func Run(ctx context.Context, s go_app.Server) error {
    g, ctx := errgroup.WithContext(ctx)
    for v := range go_app.Iter(s) {
        if sp, ok := v.(Spinner); ok {
            g.Go(func() error { return sp.Spin(ctx) })
        }
    }
    return g.Wait()
}
```

`Iter`는 이미 있다. 레이어는 `Spin`을 **쓰고 싶을 때만** 쓰고, 안 쓰는 레이어는 아무것도
안 한다 — `Overlay`에 빈 `Spin`을 상속시키는 oasys 방식과 달리 "이 레이어에 백그라운드
작업이 있는가"가 코드에 보인다.

한 가지 결정이 남는다: `Spin`이 죽으면 프로세스가 죽는가. **죽어야 한다.** GC 루프가
조용히 멈춘 서버는 며칠 뒤에 발견된다. `errgroup`이 그 답이고, `cmd/serve.go`에서
`srv.Serve(lis)`와 나란히 기다리면 된다. `health` readiness에 붙일지는 별개 — 붙이면
GC 하나가 로드밸런서에서 전체를 빼므로, 기본은 붙이지 않는 쪽.

`todo.md` #10(watch/signals)도 같은 자리에 들어온다. 두 번째 `Recorder`가 이미 허용되므로
(Phase 0.4), 발행자는 레코더로 등록되고 소비 루프는 `Spin`으로 돈다.

---

## 6. 저장소/모듈 배치

세 가지 선택지.

**(a) 같은 저장소, 같은 모듈 — `runtime/` 디렉터리.**
가장 싸다. 그러나 앱이 템플릿을 복사해 가는 순간 `runtime/`도 복사되므로 **아무것도
해결하지 못한다.** 지금 상태와 같다.

**(b) 같은 저장소, 별도 모듈 — `runtime/go.mod`.**
`protoc-gen-orm-ent`가 `runtime/`을 그렇게 갖고 있다(`go.work`가 그 저장소에 있다).
앱은 `github.com/lesomnus/go-app/runtime`을 의존하고 템플릿 자체는 복사해 간다.
태그가 `runtime/v0.1.0` 형태로 갈라진다는 사소한 불편이 있다.

**(c) 별도 저장소.**
가장 깨끗하고 가장 비싸다. 런타임과 템플릿을 함께 고치는 변경이 PR 두 개가 된다 —
`PLAN.md`가 "The generator and this repo take turns"라고 적은 그 비용을 한 겹 더 얹는
것이다.

**(b)를 권한다.** 이미 `protoc-gen-orm-ent`에서 같은 패턴을 쓰고 있고, 런타임과 템플릿이
함께 움직이는 시기가 한동안 이어질 것이므로.

---

## 7. 순서

각 단계는 그 자체로 끝나고, 다음 단계 없이도 이득이 있어야 한다.

| | 무엇 | 의존 |
| --- | --- | --- |
| **1** | `runtime/` 모듈을 만들고 **Tier 1**을 옮긴다 (`grpcx`, `migrate`, `config`, `version`, `slug`) | 없음. 순수 이동. |
| **2** | `runtime/id` — 도메인 태그 UUID, `Mint`, `Of`. §1.5의 세 지적을 반영해서 `hid`를 베끼지 말고 다시 쓴다 | 없음 |
| **3** | 생성기: `orm.message.domain` + `Minter` 훅 + 엔티티별 `<E>Mint` 스텁 | `protobuf-orm` 게시 필요 (2.2에서 겪은 그 절차) |
| **4** | 앱 적용: `core.CheckId`/`NobodyId` 제거, 감사 로그가 도메인으로 "무엇이었는가"를 답하게 | 3 |
| **5** | `goapp` CLI — `gen`, `gen --check`, `config env`, `doctor` | 1 |
| **6** | `bare.Unscoped` 제거 → 엔티티 추가 시 `Scope` 누락이 컴파일 오류 (§4 #2) | 없음, 그러나 5의 `entity add`가 있으면 훨씬 낫다 |
| **7** | **Tier 2** — `frame`/`auth`/`gate` 제네릭화, `ox` 껍데기 | 1 |
| **8** | `goapp entity add` / `layer add` | 3, 6, 7 |
| **9** | `runtime/spin` 라이프사이클 (§5) + watch (`todo.md` #10, #11) | 1 |

1~2는 서로 독립이고 오늘 시작할 수 있다. 3이 외부 게시를 기다리는 유일한 지점이다.

---

## 8. 프레임워크화가 잃는 것

솔직하게 적어 둘 것. 이 저장소의 가장 큰 자산은 **모든 결정과 그 이유가 읽히는 곳에
있다는 것**이다. README 900줄, PLAN 390줄, 그리고 코드보다 긴 주석들. 런타임 모듈로
옮긴 코드는 앱을 만든 사람이 열어 보지 않는다.

그래서:

- **런타임에도 같은 주석 규율을 지킨다.** `internal/grpcx/limit.go`가 왜 테넌트로
  세는지를 적은 문단은 옮길 때 같이 간다. 옮기면서 요약하지 않는다.
- **배선은 앱에 남긴다** (§2 Tier 3). `cmd/serve.go`가 `runtime.Serve()` 한 줄이 되는
  순간 이 저장소는 다른 프레임워크들과 같은 것이 된다.
- **README는 나뉘어야 한다.** "런타임이 무엇을 해 주는가"(런타임 저장소)와 "이 앱이
  무엇을 결정했는가"(템플릿). 지금 한 파일에 섞여 있는 것을 나누는 일 자체가 무엇이
  프레임워크이고 무엇이 앱인지를 가르는 연습이 된다.

그리고 한 가지 더 — **강제는 되돌리기 어렵다.** §4의 2번(`Unscoped` 제거)은 프레임워크가
앱의 빌드를 깨는 첫 번째 사례가 되고, 그런 항목은 늘어나기만 하고 줄지 않는다. 표에서
7번(레이어 순서)을 권하지 않은 것은 그 때문이다. 강제할 것을 고를 때의 기준은 하나여야
한다: **빠뜨렸을 때 조용히 틀리는가.** 조용히 틀리는 것만 강제한다.
