import ast

def __sango_run(code, g):
    tree = ast.parse(code, mode='exec')
    if tree.body and isinstance(tree.body[-1], ast.Expr):
        last_expr = tree.body.pop()
        exec(compile(tree, "<sango>", "exec"), g)
        return eval(compile(ast.Expression(last_expr.value), "<sango>", "eval"), g)
    
    exec(compile(tree, "<sango>", "exec"), g)
    return None