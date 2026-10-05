data "external_schema" "gorm" {
  program = [
    "go",
    "run",
    "-mod=mod",
    "./loader",
  ]
}

env "gorm" {
  src = data.external_schema.gorm.url
  dev = "docker://postgres/17/dev?search_path=public"

  migration {
    dir    = "file://migrations"
    format = golang-migrate
  }

  format {
    migrate {
      diff = "{{ sql . \"  \" }}"
    }
  }
}
