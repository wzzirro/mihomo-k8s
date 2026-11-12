# Replaces ${NAME} placeholders with values from the environment.
# Values are inserted as text; a placeholder that makes up a whole
# double-quoted scalar ("${NAME}") is escaped for YAML.
# Fails when a referenced variable is not set, so a pod never starts with a
# half-rendered configuration.
function yaml_escape(s,    i, c, r) {
  r = ""
  for (i = 1; i <= length(s); i++) {
    c = substr(s, i, 1)
    if (c == "\\" || c == "\"") r = r "\\"
    r = r c
  }
  return r
}

{
  line = $0
  out = ""
  while (match(line, /\$\{[A-Za-z_][A-Za-z0-9_]*\}/)) {
    name = substr(line, RSTART + 2, RLENGTH - 3)
    if (!(name in ENVIRON)) {
      printf "render-config: environment variable %s is not set\n", name > "/dev/stderr"
      missing = 1
    }
    value = ENVIRON[name]
    # Inside a double-quoted YAML scalar ("${NAME}") the value is escaped.
    if (substr(line, RSTART - 1, 1) == "\"" && substr(line, RSTART + RLENGTH, 1) == "\"") {
      value = yaml_escape(value)
    }
    out = out substr(line, 1, RSTART - 1) value
    line = substr(line, RSTART + RLENGTH)
  }
  print out line
}
END {
  if (missing) exit 1
}
