package editor

// RelocateImages keeps compositions and undo history attached to the same
// pictures after the host moves a document. No user edit is introduced.
func (e *Editor) RelocateImages(urls map[string]string) {
	e.Engine.RelocateImages(urls)
	if e.composition != nil {
		e.composition.Doc.MapImages(func(url string) string {
			if to, ok := urls[url]; ok {
				return to
			}
			return url
		})
	}
	if e.leafComp != nil {
		for i := range e.leafComp.state.Runs {
			if url := e.leafComp.state.Runs[i].Image.URL; url != "" {
				if to, ok := urls[url]; ok {
					e.leafComp.state.Runs[i].Image.URL = to
				}
			}
		}
		e.leafComp.committedDoc = nil
	}
	clear(e.images)
	e.ready = false
	e.sync()
}
