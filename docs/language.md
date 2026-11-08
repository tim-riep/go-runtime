# The go-runtime language

Packages contain code in the go-runtime language. Code reads like plain English where
that helps people who are not programmers, stays short for programmers, and reaches
as far as typical server code: classes, interfaces, closures, collections, checked
exceptions and exact decimals. This document describes language version 1.

Every example in this document is compiled by the tests of the repository against the
example package [TRORDER](../examples/TRORDER); blocks marked as fragments run as test
methods.

## Files and objects

Source files live in `src/` of a package, one object per file. The file name is the
name of the object without its namespace; the extension gives the kind of object:

| Extension    | Object                                                  |
|--------------|---------------------------------------------------------|
| `.class`     | class, exception class or test class                    |
| `.interface` | interface                                               |
| `.include`   | members include or statements include                   |

`src/ORDER_RATING.class` defines `/TRORDER/ORDER_RATING`. The manifest states the
language version: `"languageVersion": 1`.

```rt
# Rates a single order against a fixed limit.
public class /TRORDER/DOC_SIMPLE

  public constant LIMIT: DEC(15,2) = 500.00

  public static method IS_LARGE(order: /TRORDER/ORDER_INFO) returns BOOL
    return order.AMOUNT > LIMIT
  end

end
```

## Lexical structure

- Keywords are lower case: `class`, `method`, `if`, `for`, `raise`.
- Global names carry the namespace of their package and are upper case:
  `/TRORDER/ORDER_RATING`. A global name is a single token without spaces.
- Member names (methods, attributes, constants, components of structures, constants of
  enums) are upper case without namespace: `LIMIT`, `RATE`.
- Local names (variables and parameters) are lower case: `order`, `total`.
- The compiler rejects names in the wrong case; the formatter never renames.
- `/` right after an operand divides; anywhere else it starts a global name. The
  canonical form has spaces around binary operators, so `a / /TRORDER/X.LIMIT` divides by
  a constant. After the words `set` and `by` of database statements a global name
  follows, such as an extension field.
- Comments start with `#` and run to the end of the line.
- A statement ends with its line; there is no semicolon. A line continues after a binary
  operator, a comma, `=`, `default`, `and` or `or` at its end, and inside parentheses and
  brackets after the opening one and before the closing one.
- Strings are written in double quotes with placeholders `{expression}` and the escapes
  `\"`, `\\`, `\n`, `\t`, `\{`, `\}` and `\u{hex}`. Numbers are `42`, `1_000` or `12.50`;
  the digits after the point are the scale of a decimal literal. `null`, `true` and
  `false` are literals.

Database access uses the keywords `commit`, `rollback`, `session`, `using`, `select`,
`insert`, `update`, `delete`, `from` and `where`; `single`, `count`, `order`, `by`,
`ascending`, `descending`, `limit` and `set` are keywords only at their place in a
database statement. `transform`, `parameters` and `into` belong to the expression
`transform`. `authorization` and `authorized` belong to authorization checks; `check`
and `any` are keywords only there and remain usable as names.

## Types

| Type                       | Values                                                         |
|----------------------------|----------------------------------------------------------------|
| `STRING`                   | text of any length                                             |
| `INT4`, `INT8`             | integers of 32 and 64 bits                                     |
| `DEC(p,s)`                 | exact decimals with up to 38 digits, s of them after the point |
| `DATE`, `TIMESTAMP`        | calendar dates; points in time in UTC with microseconds        |
| `BOOL`                     | `true` and `false`                                             |
| `UUID`, `BYTES`            | UUIDs; binary data                                             |
| `T?`                       | `T` or `null`; a type without `?` is never null                |
| `array of T`               | arrays of dynamic length, positions start at 1                 |
| `map of K to V`            | maps that keep the order of insertion                          |
| `function (T) returns R`   | functions, also with `raises`                                  |
| classes and interfaces     | objects, passed by reference                                   |
| `SESSION`                  | a database session                                             |

The schema provides the other types: those of the own package and of the packages it
requires, directly as listed in the manifest.

- Structures and tables become structs with their resolved components. Components
  without `notNull` are optional; key fields of tables never are. A structure or array
  component with `notNull` is never null and starts empty. Tables have no tenant
  field, keys or foreign keys as structs. Fields that other packages add with extensions
  carry their full name, `company./TROTTO/OTTO_ID`; only the extending package and
  packages that require it use them, but every row holds them.
- New values of structs, as by `var row: /TRORDER/ORDERS` or `new`, have the defaults
  of the schema; fields without default are `null` when optional and the zero value of
  their type otherwise: `""`, `0`, `false`, `1970-01-01`, `1970-01-01T00:00:00Z` and the
  nil UUID.
- Array types become arrays of their row type. Arrays of array types with a unique key
  keep an index: `orders.LOOKUP(ID: "A1")` finds an element without searching.
- Data elements and domains become named types over their base type: `CHAR(n)` and
  `STRING` are `STRING`, `BLOB` is `BYTES`. Values of the base type can be stored in them;
  the VM checks length, `allowEmpty` and digits when a value is stored and raises
  `/RT/VALUE_ERROR` or `/RT/OVERFLOW`.
- Domains with fixed values become enums: `/TRORDER/RATING.HIGH`. A fixed value is named
  by its attribute `name` or by the value itself if it looks like a member name.
  `VALUE()` returns the fixed value, `NAME()` the name, and `/TRORDER/RATING.PARSE("H")`
  the constant or `null`. A placeholder shows the name.

Structs, arrays and maps are values: assignment, passing and returning copy them.
Objects and functions are references. `=` compares structs, arrays and maps by their
content and objects by identity.

`var x = value` derives the type; `var x: T = value` states it, and `var x: T` starts
with the zero value of `T`, which needs `T` to be no class, function or `SESSION`.
Parameters, results, attributes and constants always state their type.

`UUID.RANDOM()` returns a random UUID.

### Conversions and decimals

Implicit conversions never lose information: `INT4` to `INT8`, integers to `DEC(p,s)`
with enough digits before the point, `DEC(p,s)` to a `DEC` of at least the same scale,
`T` to `T?`, named types and their base types. Number literals take the type they are
used as when they fit. The digits of a `DEC` before the point are checked when it is
stored and raise `/RT/OVERFLOW`.

Everything else is explicit with `as`: between number types, from and to `STRING`,
between `DATE` and `TIMESTAMP`, from `UUID` to `STRING` and back, and between `STRING`
and `BYTES` in UTF-8. Invalid text and bytes that are no UTF-8 raise
`/RT/CONVERSION_ERROR`.

`DEC` arithmetic is exact. `+` and `-` keep the larger scale, `*` adds the scales, `/`
has the scale of the larger operand scale but at least 6. A smaller scale needs `as`,
which rounds half away from zero. More than 38 digits raise `/RT/OVERFLOW`. Integers raise
`/RT/OVERFLOW` instead of wrapping around; `/` on integers truncates towards zero and
`mod` has the sign of the dividend. Division by zero raises `/RT/DIVISION_BY_ZERO`.

```rt-fragment
var price: DEC(15,2) = 10.20
var share = price / 3
assert share = 3.400000
assert share as DEC(15,2) = 3.40
assert 7 / 2 = 3
assert -7 mod 3 = -1
assert "{1.005 as DEC(5,2)}" = "1.01"
```

## null and the empty string

`null` means "no value"; `""` is a value. Nothing converts one into the other.

- `x is null` and `x is not null` test for no value.
- `x is empty` tests for length zero; it applies to `STRING`, `BYTES`, arrays and maps
  and is an error on an optional type that was not checked for null.
- The compiler follows the checks through the control flow: after `if x is not null`,
  `x` is not optional in the block; after `if x is null return ... end` neither. `and`
  and `or` carry the checks to their right side. Any other use of a value that may be
  null is an error. Checks of attributes of objects end with the next call, checks of
  variables with the next assignment.
- `a default b` is `a`, or `b` if `a` is null; it is not optional if `b` is not.

```rt-fragment
var order = new /TRORDER/ORDER_INFO(ID: "A1", AMOUNT: 5.00, NOTE: "", PAID: false)
assert order.NOTE is not null and order.NOTE is empty
assert order.COUPON is null
var coupon = order.COUPON default "none"
assert coupon = "none"
if order.COUPON is null
  order.COUPON = "SPRING"
end
assert order.COUPON.LENGTH() = 6
```

## Expressions

Operators by increasing precedence:

| Operators                               | Meaning                                  |
|-----------------------------------------|------------------------------------------|
| `default`                               | value or replacement for null            |
| `or`, `and`, `not`                      | logic with short-circuit                 |
| `=`, `<>`, `<`, `>`, `<=`, `>=`, `is`   | comparison, not chained                  |
| `+`, `-`                                | addition, subtraction, joining strings   |
| `*`, `/`, `mod`                         | multiplication, division, remainder      |
| `-`                                     | negation                                 |
| `as`                                    | conversion                               |
| `.`, `(...)`, `[...]`                   | member, call, index                      |

`=` compares inside expressions and assigns as a statement of its own. `+` joins two
strings; other values appear in text through placeholders.

New values: `new /TRORDER/ORDER_RATING()` creates an object, `new /TRORDER/ORDER_INFO(ID: "1",
AMOUNT: 1.00, PAID: true)` a struct with its components by name, `new array of T` and
`new map of K to V` empty collections. `[1, 2]` and `["a": 1]` are literals; `[]` and
`[:]` need a stated type.

Functions are values. `function (x: INT4) returns INT4 ... end` has a body,
`function (x: INT4) => x * 2` a single expression. Functions use the variables of the
enclosing method, which keep changing with it, but cannot assign them.

```rt-fragment
var factor = 2
var times = function (x: INT4) => x * factor
factor = 3
assert times(5) = 15
var even = [1, 2, 3, 4].FILTER(function (n: INT4) => n mod 2 = 0)
assert even.MAP(function (n: INT4) => "#{n}").JOIN(" ") = "#2 #4"
```

Built-in methods:

- Arrays: `LENGTH`, `APPEND`, `INSERT`, `REMOVE`, `CLEAR`, `CONTAINS`, `INDEX_OF`,
  `FIND`, `FILTER`, `MAP`, `ANY`, `ALL`, `SORT`, `SORT_BY`, `REVERSE`, `SLICE`, `JOIN`,
  `LOOKUP`.
- Maps: `LENGTH`, `GET`, `SET`, `CONTAINS`, `REMOVE`, `KEYS`, `VALUES`, `CLEAR`;
  `m[key]` raises `/RT/KEY_ERROR` for a missing key, `GET` returns null.
- Strings: `LENGTH`, `UPPER`, `LOWER`, `TRIM`, `CONTAINS`, `STARTS_WITH`, `ENDS_WITH`,
  `INDEX_OF`, `SUBSTRING`, `REPLACE`, `SPLIT`.
- Dates: `ADD_DAYS`, `DAYS_UNTIL`, `YEAR`, `MONTH`, `DAY`; timestamps: `DATE`.
- Enums: `VALUE`, `NAME` and the static `PARSE`.

Methods that change their array or map (`APPEND`, `INSERT`, `REMOVE`, `CLEAR`, `SET`)
are called on a variable, attribute or component. Positions start at 1; others raise
`/RT/INDEX_ERROR`.

## Statements

`var`, assignment, `if`/`elseif`/`else`, `while`, `for x in array`, `for i, x in array`,
`for key, value in map`, `for i from a to b step c`, `break`, `continue`, `return`,
`raise`, `try`/`catch`/`finally`, `log debug|info|warning|error`, `assert` (in test
classes) and `include`. Blocks end with `end`; indentation has no meaning for the parser.

```rt-fragment
var out: array of STRING = []
for i from 10 to 1 step -3
  if i mod 2 = 0
    continue
  end
  out.APPEND("{i}")
end
var counts: map of STRING to INT4 = ["a": 1]
counts["b"] = 2
for key, value in counts
  out.APPEND("{key}{value}")
end
assert out.JOIN(",") = "7,1,a1,b2"
```

## Classes

Classes have attributes, constants, static and instance methods and one constructor.
Members are `public`, `package` (only within the package) or `private`. Members of the
own class are used without prefix or with `me.`.

Classes are final unless `open`; only `open` methods can be overridden, and overriding
needs `override` with the same signature. A class extends one class and implements
interfaces explicitly with `implements`. A constructor calls the constructor of its base
class with `super(...)` as its first statement; `super.METHOD(...)` calls the
overridden method. Attributes without initial value start with `""`, `0`, `false`, empty
collections or null for optional types; attributes of class types need an initial value or
must be optional.

```rt
public open class /TRORDER/DOC_ACCOUNT

  public attribute BALANCE: DEC(15,2)

  public open method DEPOSIT(amount: DEC(15,2))
    BALANCE = BALANCE + amount
  end

end
```

```rt
public class /TRORDER/DOC_BONUS_ACCOUNT extends /TRORDER/DOC_ACCOUNT

  public override method DEPOSIT(amount: DEC(15,2))
    super.DEPOSIT(amount + 1.00)
  end

end
```

Methods marked `executed by kernel` have no body; the kernel executes them (see
[Methods executed by kernel](#methods-executed-by-kernel)).

## Interfaces

```rt
public interface /TRORDER/DOC_PRINTER

  method PRINT(order: /TRORDER/ORDER_INFO) returns STRING

end
```

A class implements every method of its interfaces as public method with the same
signature; interfaces may extend interfaces. There are no free functions and no generic
classes; only collections and function types take type parameters.

## Exceptions

Exception classes extend `/RT/EXCEPTION` of the kernel, which has the attribute `TEXT`
and the method `MESSAGE()`. Exceptions are checked: a method declares with `raises` what
it may raise, and every raised or declared exception must be caught or declared again.
`raise X with a, b` passes the arguments to the constructor of `X`; `catch e: X` catches
`X` and its subclasses; `finally` runs on every way out of the block.

The VM raises runtime errors, which extend `/RT/RUNTIME_ERROR`: `/RT/OVERFLOW`,
`/RT/DIVISION_BY_ZERO`, `/RT/INDEX_ERROR`, `/RT/KEY_ERROR`, `/RT/DUPLICATE_KEY`,
`/RT/CONVERSION_ERROR`, `/RT/VALUE_ERROR` and `/RT/ASSERTION_FAILED`. They are not
declared; code catches them only by naming them or `/RT/RUNTIME_ERROR`, never through
`/RT/EXCEPTION`. Exceeding a limit (instructions, call depth, memory, collection length,
cancellation) ends the execution; no code can catch it and `finally` does not run.

```rt-fragment
var steps: array of STRING = []
try
  steps.APPEND("try")
  var zero = 0
  var x = 1 / zero
catch e: /RT/DIVISION_BY_ZERO
  steps.APPEND("caught")
finally
  steps.APPEND("finally")
end
assert steps.JOIN(" ") = "try caught finally"
```

## Includes

An include is an object of its own whose content is inserted where it is included. Its
first line tells what it contains:

```rt
members include /TRORDER/DOC_HELPERS

  private method DOUBLE(n: INT4) returns INT4
    return n * 2
  end

end
```

```rt
statements include /TRORDER/DOC_COUNT
  count = count + 1
end
```

`include /PKG/NAME` stands on a line of its own among the members of a class (members
include) or the statements of a block (statements include). Includes are inserted into
the syntax tree and checked at every place of use: a statements include sees the
variables of the block around it. `include /PKG/NAME if found` inserts nothing when the
include does not exist; `rt build` notes it. Includes may include includes; cycles are
errors. Messages and stack traces name the line in the include and the chain of
inclusions. An include that is included nowhere is only parsed and formatted.

```rt
public class /TRORDER/DOC_INCLUDING

  include /TRORDER/DOC_HELPERS

  public static method COUNT_TWICE() returns INT4
    var count = 0
    include /TRORDER/DOC_COUNT
    include /TRORDER/DOC_COUNT
    include /TRORDER/DOC_MISSING if found
    return count
  end

  public method USE() returns INT4
    return DOUBLE(COUNT_TWICE())
  end

end
```

`include` uses includes of the own package and of required packages. `include ... if
found` may name an include of any package, also one that is not required; it is the only
reference that needs no required package. Inserted code is checked as code of the
including package and uses only what that package may use. The kernel compiles the
including package again whenever the package of the include is installed, upgraded or
removed, so the code of the include is inserted or left out accordingly.

## Classes of other packages

Code uses the `public` classes and interfaces of required packages and their `public`
members, extends their `open` classes and implements their interfaces. `package`
members stay within their package.

## Database access

Code reads and writes the tables of the schema. The kernel adds the tenant of the
execution to every statement on a table with tenant data, so code only sees the rows of
its tenant. The examples use the table `/TRORDER/ORDERS`.

```rt
public class /TRORDER/DOC_ORDER_STORE

  public static method ADD(no: INT8, id: STRING, amount: DEC(15,2)) returns /TRORDER/ORDERS
    var order: /TRORDER/ORDERS
    order.ORDER_NO = no
    order.ID = id
    order.AMOUNT = amount
    insert order
    return order
  end

  public static method FIND(no: INT8) returns /TRORDER/ORDERS?
    return select single from /TRORDER/ORDERS where ORDER_NO = no
  end

  public static method OPEN_TOTAL() returns DEC(15,2)
    var total = 0.00
    for order in select from /TRORDER/ORDERS where PAID = false and NOTE is not null order by AMOUNT descending
      total = total + order.AMOUNT
    end
    return total
  end

  public static method LARGEST(n: INT8) returns array of /TRORDER/ORDERS
    return select from /TRORDER/ORDERS order by AMOUNT descending, ORDER_NO limit n
  end

  public static method PAY(no: INT8) raises /TRORDER/INVALID_ORDER
    var order = FIND(no)
    if order is null
      raise /TRORDER/INVALID_ORDER with "{no}"
    end
    order.PAID = true
    update order
  end

  public static method PAY_ALL(ids: array of STRING) returns INT8
    return update /TRORDER/ORDERS set PAID = true, NOTE = null where ID in ids
  end

  public static method REMOVE_PAID() returns INT8
    var count = select count from /TRORDER/ORDERS where PAID = true
    delete from /TRORDER/ORDERS where PAID = true
    return count
  end

end
```

- `select from T` returns `array of T`, `select single from T` the first row or `null`,
  and `select count from T` an `INT8`. Without `order by` rows come in the order of the
  primary key. `limit` restricts the number of rows. A select into an array reads at most
  10 000 rows and raises `/RT/TOO_MANY_ROWS` beyond that.
- `for row in select ...` reads the rows one by one through a cursor, without that limit.
  The loop may change the rows it reads.
- `insert row`, `update row` and `delete row` write one row given as value of the table
  type; `update` and `delete` find it by its primary key and raise `/RT/ROW_NOT_FOUND` when
  there is none. `update` writes all fields that are not part of the key.
- `update T set A = x, B = y where ...` and `delete from T where ...` change all matching
  rows and return their number; they may stand as statements.
- In `where`, upper-case names and global names are fields of the table; values come from
  local names, `me.X` or `/PKG/C.X`. A comparison relates a field to a value or to another
  field with `=`, `<>`, `<`, `<=`, `>` or `>=`; further there are `FIELD in array`,
  `is [not] null` and `is [not] empty`, joined by `and`, `or` and `not`. As everywhere,
  a value that may be `null` cannot be compared; `NOTE is null` and `NOTE is empty` are
  different conditions. A field that is `null` satisfies no comparison.
- Errors of the database are runtime errors under `/RT/DATABASE_ERROR`, which code catches
  by naming them: `/RT/DUPLICATE_ROW`, `/RT/FOREIGN_KEY_VIOLATION`, `/RT/CHECK_VIOLATION`,
  `/RT/NOT_NULL_VIOLATION`, `/RT/ROW_NOT_FOUND`, `/RT/TOO_MANY_ROWS`, `/RT/LOCK_TIMEOUT`
  and `/RT/SESSION_ERROR`. `TABLE_NAME` and `CONSTRAINT_NAME` tell where an error happened
  when the database reports it. After a caught error the session goes on.

## Sessions and transactions

Every execution has a default session. `open session` opens another one with a
connection and a transaction of its own; `using s` runs a statement in it. A transaction
begins with the first statement of a session; `commit` and `rollback` end it, the next
statement begins a new one. Without a session they apply to the default session.

```rt
public class /TRORDER/DOC_ORDER_IMPORT

  public static method IMPORT(orders: array of /TRORDER/ORDER_INFO) returns INT4
    var protocol = open session
    var count = 0
    for info in orders
      var order: /TRORDER/ORDERS
      order.ORDER_NO = count + 1
      order.ID = info.ID
      order.AMOUNT = info.AMOUNT
      order.PAID = info.PAID
      insert order
      count = count + 1
    end
    commit
    PROTOCOL(protocol, count)
    return count
  end

  private static method PROTOCOL(s: SESSION, count: INT4)
    var order: /TRORDER/ORDERS
    order.ORDER_NO = 0
    order.ID = "IMPORT"
    order.AMOUNT = 0.00
    order.NOTE = "{count} orders imported"
    insert order using s
    commit s
  end

end
```

- At the end of an execution the kernel commits the default session, or rolls it back
  when an exception ends the execution. The kernel may also be set to roll back whatever
  code did not commit. Other sessions that are not committed are rolled back with a
  warning in the log.
- An execution has at most four sessions, the default session included.
- `SESSION` is allowed for variables, parameters and results; attributes, constants and
  collections cannot hold it, and `rt run` cannot pass it.
- `commit` or `rollback` of a session inside a `for ... in select` of the same session
  would end the cursor of the loop: the compiler rejects it when the loop is around the
  statement, and it raises `/RT/SESSION_ERROR` when a called method does it.
- A statement waits at most two seconds for a lock and then raises `/RT/LOCK_TIMEOUT`.
  Most often another session of the same execution holds the lock.
- PostgreSQL runs every statement in a savepoint, so a session goes on after a caught
  error. SQLite allows one writing transaction at a time: when a session writes while
  another session of the execution has uncommitted changes, the statement raises
  `/RT/SESSION_ERROR` at once. Sessions of SQLite read in parallel; a reading transaction
  keeps the state of its first statement until it ends.

## Methods executed by kernel

Some methods are written in Go inside the kernel: XSLT transformations, hashing, the
clock or reading assets. Classes of kernel packages declare them with `executed by kernel`
instead of a body, and code calls them like any other method, with type checks, the
record of uses and the dependency on their package.

```
public class /TRBASE/XSLT_UTILITIES

  public static method RUN(stylesheet: BYTES, input: BYTES) returns BYTES raises /TRBASE/XSLT_FAILED executed by kernel

end
```

- Only kernel packages, which come with the kernel binary, have such methods. The compiler
  accepts a method only if the kernel registers an implementation with exactly the same
  signature: static or not, the types of the parameters with `?`, the result and the
  exceptions. Otherwise the error shows both signatures. Such methods are never `open`;
  a class may mix them with methods that have a body.
- An implementation raises the exceptions the method declares. Any other error, and a
  panic of the implementation, becomes `/RT/NATIVE_ERROR`; the kernel goes on.
- A class marked `executed by kernel` is a native class: its objects carry a value of the
  kernel that only its methods see, such as a compiled stylesheet. Native classes have no
  attributes, constants or constructor, are never `open` and extend nothing; their objects
  come from their static methods. Code holds them in variables and collections, but not in
  static attributes or constants, and `rt run` cannot pass them. The kernel frees them at
  the end of every execution.
- Every method executed by kernel runs under the time limit of the execution, 60 seconds
  by default, and inputs larger than 16 MiB raise `/RT/INPUT_TOO_LARGE`.
- Methods that act beyond computing and the database declare a capability, in this version
  only `clock`. A package lists the capabilities of the kernel methods it calls in its
  manifest, `"capabilities": ["clock"]`, also when it calls them through other methods of
  kernel packages; otherwise its code does not compile.
- Effects outside of the VM and the database run after the next commit of their session
  and are dropped by `rollback`. A failed effect is recorded; `rt effects list` shows it and
  `rt effects retry <id>` runs it again.

The kernel package `TRBASE` brings:

| Class | Methods |
|---|---|
| `/TRBASE/XSLT_UTILITIES` | `RUN(stylesheet, input)`: a stylesheet known only at runtime |
| `/TRBASE/XSLT_STYLESHEET` | native class: `COMPILE(stylesheet)` once, then `TRANSFORM(input)` many times |
| `/TRBASE/ASSET_REPOSITORY` | native class: asset repositories of the own and of required packages, see [Asset repositories](#asset-repositories) |
| `/TRBASE/REFLECTOR` | definitions of authorization objects and the activities, see [Reflector](#reflector) |
| `/TRBASE/CLOCK` | `NOW()` and `TODAY()`, capability `clock` |
| `/TRBASE/UUID` | `NEW()`: a random UUID |
| `/TRBASE/HASH` | `SHA256(data)` |

Errors of stylesheets raise the checked exception `/TRBASE/XSLT_FAILED` with `TEXT` and
`LINE_NO`, the line in the stylesheet or `null`.

## Asset repositories

`/TRBASE/ASSET_REPOSITORY` reads the asset repositories of packages (see the README for
their format). An object stands for one repository:

```
var repo = /TRBASE/ASSET_REPOSITORY.OPEN("TRFIN", "templates")

if repo.EXISTS("invoice/invoice.xsl")
  var info = repo.INFO("invoice/invoice.xsl")
  var stylesheet = repo.GET("invoice/invoice.xsl")
  var media_type = info.MIME_TYPE default "none"
  log info "Loaded {info.PATH} ({info.SIZE} bytes, {media_type})"
end

for entry in repo.LIST("invoice")
  if entry.IS_FOLDER
    continue
  end
  log info "{entry.PATH} {entry.LANGUAGES.LENGTH()} language variants"
end
```

| Method | Result |
|---|---|
| `OPEN(pkg, repository)` | static: the repository, or `ASSET_REPOSITORY_NOT_FOUND`, `ASSET_ACCESS_DENIED` |
| `LIST_REPOSITORIES(pkg)` | static: `/TRBASE/ASSET_REPOSITORY_ARRAY` of ID and label, ordered by ID |
| `PACKAGE_ID()`, `ID()` | the key of the repository |
| `EXISTS(path)` | `BOOL` for a folder or asset |
| `INFO(path)` | `/TRBASE/ASSET_INFO` |
| `LIST(path)`, `LIST_TREE(path)` | `/TRBASE/ASSET_ENTRY_ARRAY` of a folder, or of all entries below it, ordered by path |
| `GET(path)`, `GET_IN_LANGUAGE(path, language)` | the content as `BYTES` |
| `GET_TEXT(path)` | the content as `STRING`, decoded as UTF-8 |

- Paths follow the rules of asset repositories; `""` is the top level. A path that breaks
  them raises `ASSET_PATH_INVALID`, also in `EXISTS`. Language variants are no entries of
  their own.
- `GET` and `GET_TEXT` start from the language of the execution, `GET_IN_LANGUAGE` from the
  given one; then come English and the language-independent version. A missing asset or a
  folder raises `ASSET_NOT_FOUND`. An empty file gives empty `BYTES` and `""`; content that is
  not UTF-8 raises `ASSET_ENCODING_INVALID` in `GET_TEXT`.
- `/TRBASE/ASSET_INFO` has `PATH`, `IS_FOLDER`, `MIME_TYPE`, `SIZE`, `SHA256`, `ETAG`,
  `MODIFIED_AT`, `DESCRIPTION`, `LANGUAGE`, `LANGUAGES` and `HAS_NEUTRAL`. Size, hash, entity
  tag, time and `LANGUAGE` are those of the version `GET` returns, `LANGUAGE` being `null`
  for the language-independent one. Folders have `null` for media type, hash, entity tag and
  time and the size 0. `DESCRIPTION` is in the language of the execution, else in English,
  and `null` when there is none; `""` is a description. `LANGUAGES` are the languages with a
  variant, and `HAS_NEUTRAL` tells whether there is a language-independent version.
- Code reads the repositories of its own package and of the packages it requires. `OPEN`
  checks this against the package of the calling code, which the kernel takes from the
  running methods, never from arguments: the innermost method outside of kernel packages
  counts, so a method of a kernel package that another package calls reads with the rights
  of that package. Kernel packages read any package when the kernel runs their code
  directly, e.g. as handler of a service node. The object keeps the right for the rest of
  the execution.
- With two string literals `OPEN` is checked when the code compiles: the repository must
  exist and be readable. It is recorded as used, so an upgrade that removes it fails when
  the code is compiled again.
- `GET` and `GET_TEXT` load at most 16 MiB (`kernel.Config.MaxAssetRead`,
  `--max-asset-read`, `RT_MAX_ASSET_READ`) and raise `ASSET_TOO_LARGE` beyond, before
  reading. `/TRBASE/HTTP_RESPONSE.SEND_ASSET` streams an asset of any size into a response.
- An execution reads a content once, however often code asks for it. Contents up to 1 MiB
  stay in a cache of the kernel, addressed by their hash, so an upgrade never gives an old
  content.
- All exceptions are checked and extend `/TRBASE/ASSET_ERROR`, with `PACKAGE_ID` and
  `REPOSITORY`: `ASSET_REPOSITORY_NOT_FOUND`, `ASSET_ACCESS_DENIED` (with `CALLER`),
  `ASSET_NOT_FOUND`, `ASSET_PATH_INVALID`, `ASSET_ENCODING_INVALID` (with `PATH`) and
  `ASSET_TOO_LARGE` (with `PATH`, `SIZE` and `MAX_SIZE`). `raises /TRBASE/ASSET_ERROR`
  declares them all.

## Reflector

`/TRBASE/REFLECTOR` describes objects of the schema at runtime; in this version
authorization objects (see the README) and the activities of the kernel.

```
var obj = /TRBASE/REFLECTOR.AUTHORIZATION_OBJECT("/TRFIN/COMPANY")
for field in obj.FIELDS
  log info "{field.NAME}: {field.LABEL default field.NAME}"
end
if /TRBASE/REFLECTOR.ALLOWS(obj, /RT/ACTIVITY.APPROVE)
  ...
end
```

| Method | Result |
|---|---|
| `AUTHORIZATION_OBJECT(name)` | `/TRBASE/AUTHORIZATION_OBJECT`, or `/TRBASE/AUTHORIZATION_OBJECT_NOT_FOUND` |
| `AUTHORIZATION_OBJECT_EXISTS(name)` | `BOOL` |
| `AUTHORIZATION_OBJECTS(pkg)` | `/TRBASE/AUTHORIZATION_OBJECT_ARRAY` of a package, of all for `null`, ordered by name |
| `ACTIVITIES()` | `/TRBASE/ACTIVITY_ARRAY` with `CODE`, `LABEL` and `DESCRIPTION` |
| `ALLOWS(object, activity)` | whether the activity can be granted for the object |

- `/TRBASE/AUTHORIZATION_OBJECT` has `NAME`, `PACKAGE_ID`, `LABEL`, `DESCRIPTION`, `FIELDS`,
  `ACTIVITIES` and `ACTIVITY_CODES`, the codes for `CONTAINS`. A field
  (`/TRBASE/AUTHORIZATION_FIELD`) has `NAME`, `IS_ACTIVITY`, `DATA_ELEMENT`, `DOMAIN`, `TYPE`,
  `LENGTH`, the labels of the data element `SHORT_LABEL`, `LABEL`, `LONG_LABEL` and `HEADING`,
  and `VALUES` with the fixed values of the domain, for `ACTIVITY` the allowed activities.
- Texts are in the language of the execution, else in English, and `null` when there is
  none, never `""`. Fields with a domain have no labels.
- Activities are values of the enum of the kernel domain: `/RT/ACTIVITY.APPROVE`.
- Every package may read every object. With a string literal that names an object of the
  own, a required or a kernel package, `AUTHORIZATION_OBJECT` is checked when the code
  compiles and recorded as used, so an upgrade that removes the object fails when the code
  is compiled again. Other names are looked up when the code runs.

## Authorization checks

`check authorization` checks an authorization of the user of the execution and raises
`/RT/NOT_AUTHORIZED` when the user lacks it; `authorized` tells it as `BOOL`:

```
check authorization /TRFIN/COMPANY
  COMPANY_ID = company.ID
  COUNTRY = company.COUNTRY
  ACTIVITY = UPDATE
end
if authorized /TRFIN/COMPANY
  COMPANY_ID = any
  COUNTRY = "DE"
  ACTIVITY = READ
end
  ...
end
```

- The object belongs to the own package, a required one or the kernel. Every field of the
  object is given exactly once, on a line of its own, in any order.
- A value has the type of the field, its data element or domain; a value that may be `null`
  needs a null check first, and `null` is no value of a check. Literals are checked
  against the length of the field when the code compiles, other values when the check runs.
  `""` is a value like any other.
- `any` does not check the field: the user needs an authorization on the object whose other
  fields match.
- `ACTIVITY` takes the name of an activity, such as `UPDATE`, a constant `/RT/ACTIVITY.READ`
  or an expression of type `/RT/ACTIVITY`. Constants must be activities of the object.
- Without a user, for a locked user and outside the period of the user's roles every check
  fails. The role `/RT/ALL_AUTHORIZATIONS` passes every check.
- `/RT/NOT_AUTHORIZED` is a runtime error: code may catch it but does not have to declare
  it. Its attributes are `OBJECT` and `VALUES`, a map from field to value with `null` for
  `any`, and its text is in the language of the execution. Unhandled, it rolls the default
  session back, `rt run` ends with exit code 7 and http services answer 403.
- An execution reads the authorizations of its user once; every check after that runs in
  memory, in about 0.1 µs.
- The checks are recorded as uses of the object, its fields and the activities. When an
  upgrade adds or removes a field, changes the type of a field or removes an activity, the
  code is compiled again and fails at the check. `rt show authorization-object:NAME` lists
  the places.
- Native methods check with `native.Context.Authorize`.

## Transformations

A transformation is an XSLT stylesheet of a package: `transformations/INVOICE_HTML.xsl` is
`/TRFIN/INVOICE_HTML`. An optional `transformations/INVOICE_HTML.json` gives its labels per
language and what it produces:

```json
{ "name": "/TRFIN/INVOICE_HTML", "texts": { "en": { "label": "Invoice as HTML" } }, "output": "html" }
```

Installing a package compiles its stylesheets; an error stops the installation with file,
line and text. The kernel keeps them compiled for all executions of a generation. In this
version stylesheets cannot include or import other stylesheets (`xsl:include` and
`xsl:import` are rejected), and `document()` and similar functions read nothing outside the
stylesheet.

`transform` runs a transformation:

```
var html = transform invoice using /TRFIN/INVOICE_HTML parameters LANG = "de", MODE = "print"
var order = transform xml using /TRFIN/ORDER_IMPORT into /TRFIN/ORDER
```

- The source is XML as `STRING` or `BYTES`, or any value of a structure, table, array, map
  or simple type, which the kernel writes in the canonical XML format below.
- `parameters` sets top-level `xsl:param` elements of the stylesheet; each must be declared
  there, and required ones must be given. Values are of simple types and are passed in
  their text form; `null` leaves the default of the stylesheet.
- The result is a `STRING`; `into BYTES` gives bytes, and `into T` reads the result in the
  canonical format into a value of `T`.
- The transformation belongs to the own package or a required one. `transform` raises the
  checked exception `/TRBASE/XSLT_FAILED`, so the package requires `TRBASE`. A result that
  does not fit the type of `into` raises `/TRBASE/TRANSFORM_RESULT_INVALID`, a subclass,
  with the path and the reason.
- `/TRBASE/IDENTITY` copies its input: `transform value using /TRBASE/IDENTITY` gives the
  canonical XML of a value, and `transform xml using /TRBASE/IDENTITY into T` reads it back.

### JSON

`/TRBASE/JSON` is a native transformation: the kernel converts values to and from JSON
without a stylesheet, in the format of `rt run`.

```
var json = transform companies using /TRBASE/JSON
var company = transform request.BODY_TEXT() using /TRBASE/JSON into /TRFIN/COMPANIES
```

- Structures and tables are objects with their components in order, an extension field by
  its full name; arrays are lists and maps with `STRING` keys objects.
- `null` is `null` and `""` is `""`. A missing optional component is `null` when the kernel
  reads a value.
- `DEC` is a string with all digits of its scale (`"12.50"`), integers are numbers, `DATE`
  and `TIMESTAMP` are ISO 8601, `BYTES` base64 and enum constants their name.
- JSON that does not fit the type of `into` raises `/TRBASE/TRANSFORM_RESULT_INVALID` with
  the path and the reason. There are no parameters, and the output is compact.

Only kernel packages define native transformations, with `"native": "json"` in the
description file and no stylesheet.

### Canonical XML format

```xml
<value xmlns:xsi="http://www.w3.org/2001/XMLSchema-instance" type="/TRFIN/INVOICE">
  <INVOICE_ID>R-1</INVOICE_ID>
  <NOTE/>                          <!-- the empty string -->
  <REFERENCE xsi:nil="true"/>      <!-- null -->
  <TOTAL>150.50</TOTAL>
  <ITEMS><item><POSITION>1</POSITION><TEXT>Consulting</TEXT><AMOUNT>100.00</AMOUNT></item></ITEMS>
</value>
```

- The root is `<value>`; `type` names the structure or table and is not needed when the
  kernel reads a value. Components are elements in their order; an extension field
  `/PKG/FIELD` is `<PKG.FIELD>`, since `/` is not allowed in XML names.
- `null` is `xsi:nil="true"`, `""` an empty element. A missing optional component is
  `null` when the kernel reads a value; a missing other component, an unknown element or a
  text that does not fit is an error.
- Arrays have `<item>` elements, maps `<entry><key>…</key><value>…</value></entry>`. A simple
  value is the text of `<value>`.
- `DEC` has all digits of its scale, `DATE` is `2026-11-07`, `TIMESTAMP`
  `2026-11-07T10:30:00.000000Z`, `BOOL` `true` or `false`, `UUID` lower-case hex, `BYTES`
  base64, and enum constants are their fixed value.

## Handlers of http services

A handler of a service node (see the README) implements `/TRBASE/HTTP_HANDLER`:

```
public class /TRFIN/COMPANY_API implements /TRBASE/HTTP_HANDLER

  public method HANDLE(request: /TRBASE/HTTP_REQUEST, response: /TRBASE/HTTP_RESPONSE) returns BOOL raises /RT/EXCEPTION
    if request.METHOD() <> "GET"
      return false
    end
    var companies = select from /TRFIN/COMPANIES order by NAME limit 100
    response.SET_HEADER("Content-Type", "application/json")
    response.WRITE_TEXT(transform companies using /TRBASE/JSON)
    return true
  end

end
```

`true` ends the request, `false` passes it to the next handler of the node. `HANDLE` may
raise any exception; one that no handler catches ends the request with 500 and a rollback.

| `/TRBASE/HTTP_REQUEST`          | Result                                                     |
| ------------------------------- | ---------------------------------------------------------- |
| `METHOD()`                      | `GET`, `POST`, ...                                         |
| `PATH()`, `REST_PATH()`         | path of the node, and the rest of the request path below it |
| `QUERY(name)`, `QUERY_ALL(name)` | the first value of a query parameter or `null`, all values |
| `HEADER(name)`, `HEADER_ALL(name)` | the same for header fields                              |
| `BODY()`, `BODY_TEXT()`         | the body as `BYTES`, or as `STRING` if it is UTF-8         |
| `HANDLER_PARAMETER(name)`       | a parameter of the handler entry, or `null`                |

A body over `maxBodySize` raises `/RT/INPUT_TOO_LARGE`, which the kernel answers with 413
when no handler catches it.

| `/TRBASE/HTTP_RESPONSE`         | Effect                                                     |
| ------------------------------- | ---------------------------------------------------------- |
| `SET_STATUS(code)`, `STATUS()`  | status code, 200 unless set                                |
| `SET_HEADER(name, value)`, `ADD_HEADER(name, value)` | replace or add a header field          |
| `WRITE(data)`, `WRITE_TEXT(text)` | append to the body                                       |
| `SEND_ASSET(request, repository, path)` | an asset as body with the headers of `/TRBASE/ASSET_HANDLER`, streamed |

`SEND_ASSET` sends an asset of an opened `/TRBASE/ASSET_REPOSITORY` in the language of the
request: `Content-Type`, `Content-Length`, `ETag`, `Last-Modified` and `Cache-Control`, 304
for conditional requests and no body for `HEAD`. The content is streamed when the request is
done, without loading it into the memory of the execution; what was written before is dropped,
and writing afterwards fails.

The response is kept until the request ends, so status and header fields can change up to
the end; a body over `maxResponseSize` raises `/RT/INPUT_TOO_LARGE`. The status decides
about the commit: below 400 the changes of the request are committed, otherwise rolled back.

`/TRBASE/CONTEXT.USER()` is the signed-in user, `null` for anonymous requests and in
`rt run`; `TENANT()` the tenant or `null`, and `LANGUAGE()` the language of the execution.
Without tenant, statements on tenant-specific tables raise `/RT/SESSION_ERROR`. Log
statements of requests go to the log of the server with the node.

## Test classes

A test class is marked with `test`. `rt test` runs its test methods, each on a new
object; `assert condition, "message"` fails a test. Test classes are checked with the
package but are not part of the shipped program, and only test classes use them.

Every test method runs in a tenant of its own, which exists only for the method. Inside
a test, `commit` only marks the point that `rollback` returns to; at the end everything
is rolled back and the tenant is deleted. So tests start with empty tables and leave no
rows. Within a test, a session does not see what another session committed, and under
SQLite only one session writes.

```rt
public test class /TRORDER/DOC_TEST

  public test method COUNTS_TWICE()
    assert /TRORDER/DOC_INCLUDING.COUNT_TWICE() = 2, "two inclusions"
    assert new /TRORDER/DOC_INCLUDING().USE() = 4
  end

  public test method BONUS()
    var account = new /TRORDER/DOC_BONUS_ACCOUNT()
    account.DEPOSIT(10.00)
    assert account.BALANCE = 11.00
  end

end
```

## Canonical form

Only files in canonical form compile; `rt fmt` produces it and `rt fmt --check` lists
files that differ. The compiler names the first line that differs. The canonical form has
no options and does not break long lines:

- UTF-8, line feeds, no tabs, no trailing spaces, one line feed at the end of the file.
- Two spaces per block, four more for continued lines.
- One blank line after the first line of a class, interface or members include, around
  every method with a body and before the final `end`; elsewhere at most one, and none
  directly after the start or before the end of a block.
- Single spaces around binary operators, after commas and after `:`; none inside
  parentheses and brackets. Types are written `DEC(15,2)`, `T?`, `array of T`.
- Comments stay where they are, written as `# text`. Line breaks inside expressions stay;
  only their indentation changes.

## Grammar

```
file          = { comment } ( classDecl | interfaceDecl | includeDecl ) ;
classDecl     = visibility [ "test" ] [ "open" ] "class" GLOBAL [ "extends" GLOBAL ]
                [ "implements" GLOBAL { "," GLOBAL } ] [ "executed" "by" "kernel" ] NL { member } "end" ;
interfaceDecl = visibility "interface" GLOBAL [ "extends" GLOBAL { "," GLOBAL } ] NL
                { methodSig NL } "end" ;
includeDecl   = ( "members" | "statements" ) "include" GLOBAL NL ( { member } | { stmt } ) "end" ;
member        = visibility "constant" MEMBER ":" type "=" expr
              | visibility [ "static" ] "attribute" MEMBER ":" type [ "=" expr ]
              | visibility "constructor" params [ raises ] NL block "end"
              | visibility [ "static" ] [ "open" | "override" ] [ "test" ] methodSig
                ( NL block "end" | "executed" "by" "kernel" )
              | "include" GLOBAL [ "if" "found" ] ;
methodSig     = "method" MEMBER params [ "returns" type ] [ raises ] ;
params        = "(" [ LOCAL ":" type { "," LOCAL ":" type } ] ")" ;
raises        = "raises" GLOBAL { "," GLOBAL } ;
type          = ( "STRING" | "INT4" | "INT8" | "DEC" "(" INT "," INT ")" | "DATE" | "TIMESTAMP"
              | "BOOL" | "UUID" | "BYTES" | GLOBAL ) [ "?" ]
              | "array" "of" type | "map" "of" type "to" type
              | "function" "(" [ type { "," type } ] ")" [ "returns" type ] [ raises ] ;
stmt          = "var" LOCAL [ ":" type ] "=" expr | "var" LOCAL ":" type | place "=" expr | call
              | updateExpr | deleteExpr
              | "if" expr NL block { "elseif" expr NL block } [ "else" NL block ] "end"
              | "while" expr NL block "end"
              | "for" LOCAL [ "," LOCAL ] "in" expr NL block "end"
              | "for" LOCAL "from" expr "to" expr [ "step" expr ] NL block "end"
              | "break" | "continue" | "return" [ expr ] | "raise" GLOBAL [ "with" expr { "," expr } ]
              | "try" NL block { "catch" LOCAL ":" GLOBAL NL block } [ "finally" NL block ] "end"
              | "assert" expr [ "," expr ] | "log" LEVEL expr | "include" GLOBAL [ "if" "found" ]
              | ( "insert" | "update" | "delete" ) expr [ "using" expr ]
              | ( "commit" | "rollback" ) [ expr ]
              | "check" "authorization" GLOBAL NL authFields "end" ;
expr          = literal | LOCAL | MEMBER | GLOBAL "." MEMBER | "me" | "(" expr ")"
              | expr "." MEMBER | expr "(" [ args ] ")" | expr "[" expr "]"
              | "new" GLOBAL "(" [ args ] ")" | "new" ( "array" | "map" ) "of" ...
              | "[" [ expr { "," expr } ] "]" | "[" expr ":" expr { "," expr ":" expr } "]" | "[" ":" "]"
              | "function" params [ "returns" type ] [ raises ] ( "=>" expr | NL block "end" )
              | ( "-" | "not" ) expr | expr OPERATOR expr | expr "is" [ "not" ] ( "null" | "empty" )
              | expr "as" type | expr "." GLOBAL | expr "in" expr
              | selectExpr | updateExpr | deleteExpr | "open" "session" | transformExpr
              | "authorized" GLOBAL NL authFields "end" ;
authFields    = { MEMBER "=" ( "any" | expr ) NL } ;
transformExpr = "transform" expr "using" GLOBAL [ "parameters" MEMBER "=" expr { "," MEMBER "=" expr } ]
                [ "into" type ] ;
args          = expr { "," expr } | MEMBER ":" expr { "," MEMBER ":" expr } ;
selectExpr    = "select" [ "single" | "count" ] "from" GLOBAL [ "where" expr ]
                [ "order" "by" field [ "ascending" | "descending" ]
                  { "," field [ "ascending" | "descending" ] } ]
                [ "limit" expr ] [ "using" expr ] ;
updateExpr    = "update" GLOBAL "set" field "=" expr { "," field "=" expr } [ "where" expr ]
                [ "using" expr ] ;
deleteExpr    = "delete" "from" GLOBAL [ "where" expr ] [ "using" expr ] ;
field         = MEMBER | GLOBAL ;
```

## Running programs

The compiler turns a package into bytecode for a stack machine with typed instructions.
Objects and structs are laid out by the compiler: one bit per `BOOL`, two per optional
`BOOL`, a present bit per other optional field, fixed size values at fixed offsets and
references in their own slots. Every execution has limits for the number of instructions
(default 10 million), nested calls (256), allocated memory (256 MiB), the length of
collections and strings (10 million) and can be canceled.

Installing a package compiles it; its sources and the compiled code are stored in the
database, and the kernel compiles packages again when something they use changes.
`rt disasm <dir> [class or class.METHOD]` shows the bytecode of a package directory.

```bash
rt fmt examples/TRORDER
rt build examples/TRORDER
rt install examples/TRORDER
rt tenant create ACME
rt test TRORDER
rt run TRORDER /TRORDER/ORDER_RATING RATE_ALL '[[{"ID":"A1","AMOUNT":"1500.00","NOTE":"","PAID":false}]]' --tenant ACME
```

`rt run` calls public static methods of installed packages for a tenant, whose parameters and result are built-in types,
structs, enums or collections of them. JSON keeps `null` and `""` apart; `DEC` is
written as string, `DATE` and `TIMESTAMP` in ISO 8601, `BYTES` in base64 and enum
constants by name.
