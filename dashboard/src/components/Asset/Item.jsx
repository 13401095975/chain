import React from 'react';

class Item extends React.Component {
  render() {
    const item = this.props.item
    const title = item.alias ?
      `${item.alias} (${item.id})` :
      `Asset ${item.id}`

    return(
      <div className="panel panel-default">
        <div className="panel-heading">
          <strong>{title}</strong>
        </div>
        <div className="panel-body">
          <pre>
            {JSON.stringify(item, null, '  ')}
          </pre>
        </div>
      </div>
    )
  }
}

export default Item
