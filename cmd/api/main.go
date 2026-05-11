package main

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"log"
	"time"

	"github.com/gofiber/fiber/v2"
	"github.com/gofiber/template/html/v2"
	_ "github.com/mattn/go-sqlite3"
)

type Transacao struct {
	ID        int    `form:"id"`
	Valor     int    `form:"valor"`
	Natureza  string `form:"natureza"`
	Categoria string `form:"categoria"`
	Descricao string `form:"descricao"`
	Mes       string `form:"mes"`
}

type CatTotal struct {
	Categoria string
	Total     int
}

var db *sql.DB

func formatarInteiro(v int) string {
	s := fmt.Sprintf("%d", v)
	if v < 0 {
		s = fmt.Sprintf("%d", -v)
	}
	for i := len(s) - 3; i > 0; i -= 3 {
		s = s[:i] + "." + s[i:]
	}
	if v < 0 {
		return "-" + s
	}
	return s
}

func initDB() {
	var err error
	db, err = sql.Open("sqlite3", "./finance.db?_journal=WAL")
	if err != nil {
		log.Fatal(err)
	}
	schema := `
	CREATE TABLE IF NOT EXISTS flow (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		valor INTEGER, 
		natureza TEXT, 
		categoria TEXT,
		descricao TEXT,
		mes TEXT
	);`
	db.Exec(schema)
}

func main() {
	initDB()
	defer db.Close()

	engine := html.New("./views", ".html")
	app := fiber.New(fiber.Config{
		Views: engine,
	})

	app.Get("/", func(c *fiber.Ctx) error {
		mesAlvo := c.Query("mes", "2026-05")

		var meses []string
		rowsMeses, _ := db.Query(`SELECT DISTINCT mes FROM flow ORDER BY mes ASC`)
		for rowsMeses.Next() {
			var mes string
			rowsMeses.Scan(&mes)
			meses = append(meses, mes)
		}
		rowsMeses.Close()

		if len(meses) == 0 {
			meses = []string{mesAlvo}
		}

		t, _ := time.Parse("2006-01", meses[len(meses)-1])
		proximoMes := t.AddDate(0, 1, 0).Format("2006-01")
		meses = append(meses, proximoMes)
		rows, err := db.Query("SELECT id, valor, natureza, categoria, descricao, mes FROM flow WHERE mes = ? ORDER BY id DESC", mesAlvo)
		if err != nil {
			return err
		}
		defer rows.Close()

		var transacoes []Transacao
		var totalGanhos, totalGastos, totalInvestido int

		for rows.Next() {
			var t Transacao
			rows.Scan(&t.ID, &t.Valor, &t.Natureza, &t.Categoria, &t.Descricao, &t.Mes)
			transacoes = append(transacoes, t)

			if t.Natureza == "entrada" {
				totalGanhos += t.Valor
			} else {
				totalGastos += t.Valor
			}
			if t.Categoria == "investimento" {
				totalInvestido += t.Valor
			}
		}

		saldo := totalGanhos - totalGastos

		var mensalEntrada, mensalSaida int
		_ = db.QueryRow(`
			SELECT 
				SUM(CASE WHEN natureza = 'entrada' THEN valor ELSE 0 END), 
				SUM(CASE WHEN natureza = 'saida' THEN valor ELSE 0 END) 
			FROM flow WHERE mes = ?`, mesAlvo).Scan(&mensalEntrada, &mensalSaida)

		fluxoMensal := []int{mensalEntrada, mensalSaida}
		fluxoJSON, _ := json.Marshal(fluxoMensal)

		var entradaPorCat []CatTotal
		rowsEnt, _ := db.Query(`
			SELECT categoria, SUM(valor) 
			FROM flow 
			WHERE mes = ? AND natureza = 'entrada'
			GROUP BY categoria`, mesAlvo)
		for rowsEnt.Next() {
			var ct CatTotal
			rowsEnt.Scan(&ct.Categoria, &ct.Total)
			entradaPorCat = append(entradaPorCat, ct)
		}
		rowsEnt.Close()

		var saidaPorCat []CatTotal
		rowsSai, _ := db.Query(`
			SELECT categoria, SUM(valor) 
			FROM flow 
			WHERE mes = ? AND natureza = 'saida'
			GROUP BY categoria`, mesAlvo)
		for rowsSai.Next() {
			var ct CatTotal
			rowsSai.Scan(&ct.Categoria, &ct.Total)
			saidaPorCat = append(saidaPorCat, ct)
		}
		rowsSai.Close()

		entradaCatJSON, _ := json.Marshal(entradaPorCat)
		saidaCatJSON, _ := json.Marshal(saidaPorCat)

		return c.Render("index", fiber.Map{
			"Transacoes":     transacoes,
			"Ganhos":         formatarInteiro(totalGanhos),
			"Gastos":         formatarInteiro(totalGastos),
			"Investido":      formatarInteiro(totalInvestido),
			"Saldo":          formatarInteiro(saldo),
			"SaldoNegativo":  saldo < 0,
			"MesAtual":       mesAlvo,
			"DadosBarraJSON": string(fluxoJSON),
			"EntradaCatJSON": string(entradaCatJSON),
			"SaidaCatJSON":   string(saidaCatJSON),
			"MesesDoAno":     meses,
		})
	})

	app.Post("/add", func(c *fiber.Ctx) error {
		t := new(Transacao)
		if err := c.BodyParser(t); err != nil {
			return err
		}

		_, err := db.Exec("INSERT INTO flow (valor, natureza, categoria, descricao, mes) VALUES (?, ?, ?, ?, ?)",
			t.Valor, t.Natureza, t.Categoria, t.Descricao, t.Mes)
		if err != nil {
			return err
		}

		return c.Redirect("/?mes="+t.Mes, fiber.StatusSeeOther)
	})

	app.Get("/edit/:id", func(c *fiber.Ctx) error {
		id := c.Params("id")

		var t Transacao
		err := db.QueryRow("SELECT id, valor, natureza, categoria, descricao, mes FROM flow WHERE id = ?", id).
			Scan(&t.ID, &t.Valor, &t.Natureza, &t.Categoria, &t.Descricao, &t.Mes)
		if err != nil {
			return err
		}

		return c.Render("edit", fiber.Map{
			"Transacao": t,
		})
	})

	app.Post("/edit/:id", func(c *fiber.Ctx) error {
		id := c.Params("id")

		t := new(Transacao)
		if err := c.BodyParser(t); err != nil {
			return err
		}

		_, err := db.Exec("UPDATE flow SET valor=?, natureza=?, categoria=?, descricao=?, mes=? WHERE id=?",
			t.Valor, t.Natureza, t.Categoria, t.Descricao, t.Mes, id)
		if err != nil {
			return err
		}

		return c.Redirect("/?mes="+t.Mes, fiber.StatusSeeOther)
	})

	app.Post("/delete/:id", func(c *fiber.Ctx) error {
		id := c.Params("id")
		mes := c.FormValue("mes")

		_, err := db.Exec("DELETE FROM flow WHERE id = ?", id)
		if err != nil {
			return err
		}

		return c.Redirect("/?mes="+mes, fiber.StatusSeeOther)
	})

	log.Fatal(app.Listen(":3001"))
}
