let dialog = document.getElementById( 'dialog' );
if ( dialog )
  fetch( '/list' ).then( res => res.json() )
    .then( data => {
      console.log( data );
    } );

const handleKey = ( name, event ) => {
  console.log( event );
  fetch( `/track/${ name }`, {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify( {
      key: event.key,
      code: event.code,
      ctrlKey: event.ctrlKey,
      shiftKey: event.shiftKey,
      altKey: event.altKey,
    }
    )
  } ).catch( e => console.log( e ) );
};

window.addEventListener( 'keydown', e => handleKey( "keydown", e ) );
window.addEventListener( 'keyup', e => handleKey( "keyup", e ) );
window.addEventListener( 'keypress', e => handleKey( "keypress", e ) );