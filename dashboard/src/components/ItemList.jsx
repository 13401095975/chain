import React from 'react'
import { pluralize, capitalize } from '../utility/string'

import Pagination from "./Pagination"
import PageHeader from "./PageHeader/PageHeader"
import SearchBar from "./SearchBar/SearchBar"

class ItemList extends React.Component {
  componentWillMount() {
    if (this.props.currentPage === -1) {
      this.props.getNextPage()
    }
  }

  componentWillReceiveProps(nextProps) {
    if (nextProps.currentPage === -1) {
      nextProps.getNextPage()
    }
  }

  render() {
    const label = this.props.label || pluralize(capitalize(this.props.type))
    const keyProp = this.props.keyProp || "id"

    let pageHeader = <PageHeader key='page-title'
      title={label}
      buttonAction={this.props.showCreate}
      buttonLabel="New"
      showActionButton={!this.props.skipCreate}
    />

    let header = [pageHeader]
    if (!this.props.skipQuery) { header.push(
      <SearchBar key='search-bar'
        submitQuery={this.props.submitQuery}
        {...this.props.searchState}
      />
    )}

    if (this.props.pages[this.props.currentPage] !== undefined) {
      let pagination = <Pagination
          currentPage={this.props.currentPage}
          lastPage={this.props.pages[this.props.currentPage].last_page}
          getPrevPage={this.props.getPrevPage}
          getNextPage={this.props.getNextPage} />

      return(
        <div>
          {header}
          {pagination}

          {this.props.pages[this.props.currentPage].items.map((item) => {
            return <this.props.listItemComponent key={item[keyProp]} item={item} {...this.props.itemActions}/>
          })}

          {pagination}
        </div>
       )
    } else {
      return(
        <div>
          {header}

          <div className="jumbotron text-center">
            <p>No results</p>
          </div>
        </div>
      )
    }
  }
}

export default ItemList
