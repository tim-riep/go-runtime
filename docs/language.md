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
  a constant.
- Comments start with `#` and run to the end of the line.
- A statement ends with its line; there is no semicolon. A line continues after a binary
  operator, a comma, `=`, `default`, `and` or `or` at its end, and inside parentheses and
  brackets after the opening one and before the closing one.
- Strings are written in double quotes with placeholders `{expression}` and the escapes
  `\"`, `\\`, `\n`, `\t`, `\{`, `\}` and `\u{hex}`. Numbers are `42`, `1_000` or `12.50`;
  the digits after the point are the scale of a decimal literal. `null`, `true` and
  `false` are literals.

Reserved for database access in a later language part: `commit`, `rollback`, `session`,
`using`, `select`, `insert`, `update`, `delete`, `from`, `where`. `commit` and `rollback`
already parse as statements and are rejected by the compiler until then. `transform`,
`parameters` and `into` are reserved for later language parts as well.

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

The schema of the package provides the other types. In this language version only
objects of the own package are available:

- Structures and tables become structs with their resolved components. Components
  without `notNull` are optional; key fields of tables never are. Tables have no tenant
  field, keys or foreign keys as structs.
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

`var x = value` derives the type; `var x: T = value` states it. Parameters, results,
attributes and constants always state their type.

### Conversions and decimals

Implicit conversions never lose information: `INT4` to `INT8`, integers to `DEC(p,s)`
with enough digits before the point, `DEC(p,s)` to a `DEC` of at least the same scale,
`T` to `T?`, named types and their base types. Number literals take the type they are
used as when they fit. The digits of a `DEC` before the point are checked when it is
stored and raise `/RT/OVERFLOW`.

Everything else is explicit with `as`: between number types, from and to `STRING`,
between `DATE` and `TIMESTAMP`, from `UUID` to `STRING` and back. Invalid text raises
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

Methods marked `executed by kernel` have no body; they come with native methods in a
later language part and are rejected until then.

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

In this language version, includes come from the own package.

## Test classes

A test class is marked with `test`. `rt test` runs its test methods, each on a new
object; `assert condition, "message"` fails a test. Test classes are checked with the
package but are not part of the shipped program, and only test classes use them.

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
                [ "implements" GLOBAL { "," GLOBAL } ] NL { member } "end" ;
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
stmt          = "var" LOCAL [ ":" type ] "=" expr | place "=" expr | call
              | "if" expr NL block { "elseif" expr NL block } [ "else" NL block ] "end"
              | "while" expr NL block "end"
              | "for" LOCAL [ "," LOCAL ] "in" expr NL block "end"
              | "for" LOCAL "from" expr "to" expr [ "step" expr ] NL block "end"
              | "break" | "continue" | "return" [ expr ] | "raise" GLOBAL [ "with" expr { "," expr } ]
              | "try" NL block { "catch" LOCAL ":" GLOBAL NL block } [ "finally" NL block ] "end"
              | "assert" expr [ "," expr ] | "log" LEVEL expr | "include" GLOBAL [ "if" "found" ]
              | "commit" | "rollback" ;
expr          = literal | LOCAL | MEMBER | GLOBAL "." MEMBER | "me" | "(" expr ")"
              | expr "." MEMBER | expr "(" [ args ] ")" | expr "[" expr "]"
              | "new" GLOBAL "(" [ args ] ")" | "new" ( "array" | "map" ) "of" ...
              | "[" [ expr { "," expr } ] "]" | "[" expr ":" expr { "," expr ":" expr } "]" | "[" ":" "]"
              | "function" params [ "returns" type ] [ raises ] ( "=>" expr | NL block "end" )
              | ( "-" | "not" ) expr | expr OPERATOR expr | expr "is" [ "not" ] ( "null" | "empty" )
              | expr "as" type ;
args          = expr { "," expr } | MEMBER ":" expr { "," MEMBER ":" expr } ;
```

## Running programs

The compiler turns a package into bytecode for a stack machine with typed instructions.
Objects and structs are laid out by the compiler: one bit per `BOOL`, two per optional
`BOOL`, a present bit per other optional field, fixed size values at fixed offsets and
references in their own slots. Every execution has limits for the number of instructions
(default 10 million), nested calls (256), allocated memory (256 MiB), the length of
collections and strings (10 million) and can be canceled.

```bash
rt fmt examples/TRORDER
rt build examples/TRORDER
rt test examples/TRORDER
rt run examples/TRORDER /TRORDER/ORDER_RATING RATE_ALL '[[{"ID":"A1","AMOUNT":"1500.00","NOTE":"","PAID":false}]]'
```

`rt run` calls public static methods whose parameters and result are built-in types,
structs, enums or collections of them. JSON keeps `null` and `""` apart; `DEC` is
written as string, `DATE` and `TIMESTAMP` in ISO 8601, `BYTES` in base64 and enum
constants by name.
