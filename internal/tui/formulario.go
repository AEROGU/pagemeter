package tui

import (
	"fmt"
	"strings"

	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"

	"github.com/AEROGU/pagemeter/internal/config"
	"github.com/AEROGU/pagemeter/internal/escaneo"
)

// abrirFormulario agrega (ipEditar == "") o edita una impresora.
func (u *UI) abrirFormulario(ipEditar string) {
	editar := ipEditar != ""
	var orig config.Printer
	if editar {
		i := u.cfg.Buscar(ipEditar)
		if i < 0 {
			return
		}
		orig = u.cfg.Printers[i]
	} else {
		orig = config.Printer{Origen: config.OrigenManual, Habilitada: true, Marca: "auto"}
	}

	marcas := u.marcas()
	marcaIdx := 0
	for i, m := range marcas {
		if m == orig.Marca {
			marcaIdx = i
		}
	}

	form := tview.NewForm()
	resultado := tview.NewTextView().SetDynamicColors(true).SetWrap(true)
	resultado.SetBorder(true).SetTitle(" Prueba ")
	resultado.SetText("[gray]Pulsa «Probar» para comprobar que la impresora responde por SNMP.")
	// Si un clic deja el foco en este panel, cualquier tecla de salida vuelve al formulario.
	resultado.SetDoneFunc(func(tcell.Key) { u.app.SetFocus(form) })

	form.AddInputField("IP", orig.IP, 20, nil, nil)
	form.AddInputField("Nombre (opcional)", orig.Nombre, 50, nil, nil)
	form.AddDropDown("Marca", marcas, marcaIdx, nil)
	form.AddCheckbox("Habilitada", orig.Habilitada, nil)

	leer := func() config.Printer {
		p := orig
		p.IP = strings.TrimSpace(form.GetFormItemByLabel("IP").(*tview.InputField).GetText())
		p.Nombre = strings.TrimSpace(form.GetFormItemByLabel("Nombre (opcional)").(*tview.InputField).GetText())
		_, p.Marca = form.GetFormItemByLabel("Marca").(*tview.DropDown).GetCurrentOption()
		p.Habilitada = form.GetFormItemByLabel("Habilitada").(*tview.Checkbox).IsChecked()
		return p
	}
	cerrar := func() {
		u.pages.RemovePage(pgFormulario)
		u.ir(pgImpresoras)
	}

	probando := false
	form.AddButton("Probar", func() {
		p := leer()
		if err := config.ValidarIP(p.IP); err != nil {
			resultado.SetText("[red]" + tview.Escape(err.Error()))
			return
		}
		if probando {
			return
		}
		probando = true
		resultado.SetText("[yellow]Probando " + p.IP + "…")
		cfg, ctx := u.cfg.Clone(), u.ctx
		go func() {
			res, err := escaneo.Probar(ctx, cfg, u.reg, u.dial, p.IP, p.Marca)
			u.app.QueueUpdateDraw(func() {
				probando = false
				if err != nil {
					resultado.SetText("[red]No respondió:[-] " + tview.Escape(err.Error()))
					return
				}
				resultado.SetText("[green]" + tview.Escape(textoPrueba(p.Etiqueta(), p.IP, res)))
				// Si no tiene nombre, propone el modelo.
				campo := form.GetFormItemByLabel("Nombre (opcional)").(*tview.InputField)
				if strings.TrimSpace(campo.GetText()) == "" && res.Modelo != "" {
					campo.SetText(res.Modelo)
				}
			})
		}()
	})
	form.AddButton("Guardar", func() {
		p := leer()
		respaldo := u.cfg.Clone().Printers
		var err error
		if editar {
			err = u.cfg.Editar(ipEditar, p)
		} else {
			err = u.cfg.Agregar(p)
		}
		if err != nil {
			resultado.SetText("[red]" + tview.Escape(err.Error()))
			return
		}
		if err := u.cfg.Save(); err != nil {
			u.cfg.Printers = respaldo
			resultado.SetText("[red]No se pudo guardar: " + tview.Escape(err.Error()))
			return
		}
		if editar && ipEditar != p.IP {
			delete(u.estado, ipEditar)
		}
		cerrar()
	})
	form.AddButton("Cancelar", cerrar)
	form.SetCancelFunc(cerrar)

	titulo := " Agregar impresora por IP "
	if editar {
		titulo = fmt.Sprintf(" Editar %s (origen: %s) ", orig.Etiqueta(), orig.Origen)
	}
	form.SetBorder(true).SetTitle(titulo)

	caja := tview.NewFlex().SetDirection(tview.FlexRow).
		AddItem(form, 13, 0, true).
		AddItem(resultado, 0, 1, false)
	// Centrado.
	centro := tview.NewFlex().
		AddItem(nil, 0, 1, false).
		AddItem(tview.NewFlex().SetDirection(tview.FlexRow).
			AddItem(nil, 0, 1, false).
			AddItem(caja, 24, 0, true).
			AddItem(nil, 0, 1, false), 80, 0, true).
		AddItem(nil, 0, 1, false)

	if u.pages.HasPage(pgFormulario) {
		u.pages.RemovePage(pgFormulario)
	}
	u.pages.AddPage(pgFormulario, centro, true, true)
	u.dialogoActual = caja
	u.app.SetFocus(form)
	u.actualizarBarras()
}
