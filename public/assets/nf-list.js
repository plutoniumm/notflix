class SearchableList extends HTMLElement {
  constructor () {
    super();
    this._items = [];
  }

  connectedCallback () {
    this.attachShadow( { mode: 'open' } );
    this.shadowRoot.innerHTML = `
    <style>
      .search {
        width: 100%;
        font-size: 1em;
        padding: 12px;
        border-radius: 12px
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
      }
      }
    </style>
    <input
      type="text" class="search" placeholder="Search..."
    >
    <ul>
      <slot></slot>
    </ul>
    `;

    const input = this.shadowRoot.querySelector( 'input' );
    input.addEventListener( 'keyup', this._handleSearch.bind( this ) );
  }

  _handleSearch ( event ) {
    const filter = event.target.value.toLowerCase();
    const items = this.querySelectorAll( 'li' );

    if ( !filter.trim() ) {
      items.forEach( item => ( item.style.display = "" ) );
      return;
    }

    function fuzzyMatch ( pattern, str ) {
      pattern = pattern.toLowerCase();
      str = str.toLowerCase();

      let patternIdx = 0;
      let strIdx = 0;
      let score = 0;
      let consecutiveMatches = 0;

      if ( pattern.length === 0 ) return { matched: true, score: 1 };

      while ( patternIdx < pattern.length && strIdx < str.length ) {
        if ( pattern[ patternIdx ] === str[ strIdx ] ) {
          patternIdx++;
          consecutiveMatches++;
          score += consecutiveMatches;
        } else {
          consecutiveMatches = 0;
          score -= 0.1;
        }
        strIdx++;
      }

      return {
        matched: patternIdx === pattern.length,
        score: patternIdx === pattern.length ? score / str.length : 0
      };
    }

    const results = [];

    items.forEach( item => {
      const link = item.querySelector( 'a' );
      if ( !link ) return;

      const text = link.textContent || link.innerText;
      const matchResult = fuzzyMatch( filter, text );

      if ( matchResult.matched ) {
        results.push( {
          element: item,
          score: matchResult.score
        } );
      } else {
        item.style.display = "none";
      }
    } );

    results.sort( ( a, b ) => b.score - a.score );
    results.forEach( result => {
      result.element.style.display = "";
    } );
  }
}

customElements.define( 'nf-list', SearchableList );