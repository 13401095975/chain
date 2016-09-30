import chain from '../chain'
import { context, pageSize } from '../utility/environment'
import actionCreator from './actionCreator'

export default function(type, options = {}) {
  const className = options.className || type.charAt(0).toUpperCase() + type.slice(1)

  const incrementPage = actionCreator(`INCREMENT_${type.toUpperCase()}_PAGE`)
  const decrementPage = actionCreator(`DECREMENT_${type.toUpperCase()}_PAGE`)
  const receivedItems = actionCreator(`RECEIVED_${type.toUpperCase()}_ITEMS`, param => ({ param }) )
  const appendPage = actionCreator(`APPEND_${type.toUpperCase()}_PAGE`, param => ({ param }) )
  const updateQuery = actionCreator(`UPDATE_${type.toUpperCase()}_QUERY`, param => ({ param }) )
  const didLoadAutocomplete = actionCreator(`DID_LOAD_${type.toUpperCase()}_AUTOCOMPLETE`)

  const deleteItemSuccess = actionCreator(`DELETE_${type.toUpperCase()}`, id => ({ id }))
  const deleteItem = function(id) {
    return (dispatch) => chain[className].delete(context, id)
      .then(() => {
        dispatch(deleteItemSuccess(id))
      })
  }

  const getNextPageSlice = function(getState) {
    const pageStart = (getState()[type].listView.pageIndex + 1) * pageSize
    return getState()[type].listView.itemIds.slice(pageStart, pageStart + pageSize)
  }

  const fetchItems = function(params) {
    const requiredParams = options.requiredParams || {}

    params = { ...params, ...requiredParams }

    return function(dispatch) {
      const promise = chain[className].query(context, params)

      promise.then(
        (param) => dispatch(receivedItems(param))
      )

      return promise
    }
  }

  const fetchAll = function(stepCallback = () => {}) {
    return function(dispatch) {
      const fetchUntilLastPage = (next) => {
        return dispatch(fetchItems(next)).then((resp) => {
          stepCallback(resp)

          if (resp.last_page) {
            return resp
          } else {
            return fetchUntilLastPage(resp.next)
          }
        })
      }

      return fetchUntilLastPage({})
    }
  }

  const fetchQueryPage = function() {
    return function(dispatch, getState) {
      let latestResponse = getState()[type].listView.cursor
      let promise
      let filter = ''

      if (latestResponse && latestResponse.last_page) {
        return Promise.resolve({})
      } else if (latestResponse.nextPage) {
        promise = latestResponse.nextPage(context)
      } else {
        let params = {}

        if (getState()[type].listView.query) {
          filter = getState()[type].listView.query
          params.filter = filter
        }

        if (getState()[type].listView.sumBy) {
          params.sum_by = getState()[type].listView.sumBy.split(',')
        }

        promise = dispatch(fetchItems(params))
      }

      return promise.then(
        (response) => dispatch(appendPage(response))
      ).catch(( err ) => {
        if (options.defaultKey && filter.indexOf('\'') < 0 && filter.indexOf('=') < 0) {
          dispatch(updateQuery(`${options.defaultKey}='${filter}'`))
          dispatch(fetchQueryPage())
        } else {
          return dispatch({type: 'ERROR', payload: err})
        }
      })
    }
  }

  return {
    appendPage,
    updateQuery,
    fetchItems,
    deleteItem,
    fetchAll,
    incrementPage: function() {
      return function(dispatch, getState) {
        const nextPage = getNextPageSlice(getState)

        if (nextPage.length < pageSize) {
          let fetchPromise = dispatch(fetchQueryPage())

          if (nextPage.length != 0) {
            dispatch(incrementPage())
          } else if (getState()[type].listView.pageIndex != 0) {
            fetchPromise.then(() => {
              dispatch(incrementPage())
            })
          }

          return fetchPromise
        } else {
          return dispatch(incrementPage())
        }
      }
    },
    decrementPage,
    didLoadAutocomplete,
  }
}
