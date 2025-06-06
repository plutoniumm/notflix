class SearchableList extends HTMLElement {
  constructor () {
    super();
    this._items = [];
  }

  connectedCallback () {
    this.attachShadow( { mode: 'open' } );
    this.shadowRoot.innerHTML = /*html*/`
    <style>
      input[type="text"] {
        width: 100%;
        font-size: 1em;
        padding: 12px;
        border-radius: 12px;
        margin-bottom: 2em;
        background: transparent;
        color: #fff;
        border: none;
        outline: none;
        border-bottom: 2px solid #fff;
      }

      ul {
        list-style-type: none;
        padding: 0;
        margin: 0;
        height: 100%;
        overflow-y: scroll;
      }
    </style>
    <input type="text" placeholder="Search..." />
    <ul>
      <slot></slot>
    </ul>
    `;

    const input = this.shadowRoot.querySelector( 'input' );
    input.addEventListener( 'keyup', this._handleSearch.bind( this ) );
  }

  fuzzyMatch ( pattern, str ) {
    pattern = pattern.toLowerCase();
    str = str.toLowerCase();

    let pIdx = 0;
    let strIdx = 0;
    let score = 0;
    let consec = 0;

    if ( pattern.length === 0 ) return { matched: true, score: 1 };

    while ( pIdx < pattern.length && strIdx < str.length ) {
      if ( pattern[ pIdx ] === str[ strIdx ] ) {
        pIdx++;
        consec++;
        score += consec;
      } else {
        consec = 0;
        score -= 0.1;
      }
      strIdx++;
    }

    let matched = pIdx === pattern.length;
    return {
      matched, score: matched ? score / str.length : 0
    };
  }

  _handleSearch ( event ) {
    const filter = event.target.value.toLowerCase();
    const items = this.querySelectorAll( 'li' );

    if ( !filter.trim() ) {
      items.forEach( item => ( item.style.display = "block" ) );
      return;
    }

    const results = [];
    items.forEach( item => {
      const link = item.querySelector( 'a' );
      if ( !link ) return;

      const text = link.textContent || link.innerText;
      const mRes = this.fuzzyMatch( filter, text );

      if ( mRes.matched ) {
        results.push( {
          element: item,
          score: mRes.score
        } );
      } else {
        item.style.setProperty( 'display', 'none', 'important' );
      }
    } );

    results.sort( ( a, b ) => b.score - a.score );
    results.forEach( ( { element } ) => {
      element.style.display = "";
      element.parentNode.appendChild( element );
    } );
  }
}

customElements.define( 'nf-list', SearchableList );